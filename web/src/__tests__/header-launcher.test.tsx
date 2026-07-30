import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { vi } from "vitest";
import HeaderLauncher from "../grid/HeaderLauncher";
import type { Server } from "../servers";

const servers: Server[] = [
  { id: "local", origin: "http://local.test", name: "local" },
  { id: "r1", origin: "http://remote.test", name: "box-a" },
];
const localTools = [{ id: 1, name: "claude", command: "claude" }];
const localDirs = [{ id: 7, name: "multimux", path: "/repos/multimux" }];

afterEach(() => {
  vi.restoreAllMocks();
});

function mockLocalDaemon() {
  return vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const url = String(input);
    if (url.includes("/api/tools")) return new Response(JSON.stringify(localTools));
    if (url.includes("/api/dirs")) return new Response(JSON.stringify(localDirs));
    if (url.includes("/api/sessions") && (init?.method ?? "GET") === "POST")
      return new Response(
        JSON.stringify({ id: 3, tmuxName: "mm-3", toolId: 1, dir: "/repos/multimux/web", status: "running" }),
        { status: 201 },
      );
    return new Response("[]");
  });
}

test("the subdir input is sent with the launch", async () => {
  const fetchMock = mockLocalDaemon();
  render(<HeaderLauncher servers={[servers[0]]} onLaunched={vi.fn()} />);

  const subdir = await screen.findByLabelText<HTMLInputElement>("subdirectory");
  fireEvent.change(subdir, { target: { value: "web" } });
  fireEvent.click(screen.getByText("+ New"));

  await waitFor(() => {
    const post = fetchMock.mock.calls.find(([, init]) => init?.method === "POST");
    expect(post && JSON.parse(String(post[1]?.body))).toEqual({ toolId: 1, dirId: 7, subdir: "web" });
  });
});

// A subdir names a path under the selected daemon's directory, so carrying it
// across a server switch would launch somewhere the user never asked for.
test("switching servers clears the subdir", async () => {
  const fetchMock = mockLocalDaemon();
  render(<HeaderLauncher servers={servers} onLaunched={vi.fn()} />);

  fireEvent.change(await screen.findByLabelText("subdirectory"), { target: { value: "web" } });
  fireEvent.change(screen.getByLabelText("server"), { target: { value: "r1" } });
  fireEvent.change(screen.getByLabelText("server"), { target: { value: "local" } });

  await waitFor(() => expect(screen.getByLabelText<HTMLInputElement>("subdirectory").value).toBe(""));
  fireEvent.click(screen.getByText("+ New"));
  await waitFor(() => {
    const post = fetchMock.mock.calls.find(([, init]) => init?.method === "POST");
    expect(post && JSON.parse(String(post[1]?.body)).subdir).toBe("");
  });
});

test("switching servers clears the previous daemon's tools and dirs until the new fetch resolves", async () => {
  // The remote daemon's tools/dirs stay pending so the switch can be observed
  // mid-flight.
  const pending: Array<() => void> = [];
  const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const url = String(input);
    if (url.startsWith("http://remote.test")) {
      await new Promise<void>((resolve) => pending.push(resolve));
      return new Response("[]");
    }
    if (url.includes("/api/tools")) return new Response(JSON.stringify(localTools));
    if (url.includes("/api/dirs")) return new Response(JSON.stringify(localDirs));
    if (url.includes("/api/sessions") && (init?.method ?? "GET") === "POST")
      return new Response(JSON.stringify({ id: 3, tmuxName: "mm-3", toolId: 1, dir: "/a", status: "running" }), {
        status: 201,
      });
    return new Response("[]");
  });
  const onLaunched = vi.fn();

  render(<HeaderLauncher servers={servers} onLaunched={onLaunched} />);

  const toolSelect = await screen.findByLabelText<HTMLSelectElement>("tool");
  await waitFor(() => expect(toolSelect.value).toBe("1"));
  const button = screen.getByText<HTMLButtonElement>("+ New");
  expect(button).toBeEnabled();

  fireEvent.change(screen.getByLabelText("server"), { target: { value: "r1" } });

  // Tool/dir ids are per-daemon autoincrements: the local daemon's options must
  // not survive the switch, or "+ New" would POST id 1 to a daemon where it
  // means a different tool.
  expect(screen.queryByLabelText("tool")).toBeNull();
  expect(screen.queryByLabelText("dir")).toBeNull();
  expect(screen.queryByText("claude")).toBeNull();
  expect(screen.queryByText("multimux")).toBeNull();
  expect(button).toBeDisabled();
  // Empty lists mid-load are not "nothing configured": no misleading hint.
  expect(screen.queryByText(/add tools/)).toBeNull();
  expect(screen.queryByText(/add dirs/)).toBeNull();

  fireEvent.click(button);
  expect(fetchMock.mock.calls.some(([, init]) => init?.method === "POST")).toBe(false);
  expect(onLaunched).not.toHaveBeenCalled();

  // Once the remote fetch resolves empty, the Settings hint is correct.
  pending.forEach((resolve) => resolve());
  await waitFor(() => expect(screen.getByText(/add tools/)).toBeInTheDocument());
  expect(button).toBeDisabled();
});

test("failed launch displays error and leaves + New enabled so user can edit inputs and retry", async () => {
  let postCount = 0;
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const url = String(input);
    if (url.includes("/api/tools")) return new Response(JSON.stringify(localTools));
    if (url.includes("/api/dirs")) return new Response(JSON.stringify(localDirs));
    if (url.includes("/api/sessions") && (init?.method ?? "GET") === "POST") {
      postCount++;
      if (postCount === 1) {
        return new Response(JSON.stringify({ error: "directory invalid/path does not exist" }), { status: 400 });
      }
      return new Response(
        JSON.stringify({ id: 3, tmuxName: "mm-3", toolId: 1, dir: "/repos/multimux/web", status: "running" }),
        { status: 201 },
      );
    }
    return new Response("[]");
  });

  const onLaunched = vi.fn();
  render(<HeaderLauncher servers={[servers[0]]} onLaunched={onLaunched} />);

  const subdir = await screen.findByLabelText<HTMLInputElement>("subdirectory");
  fireEvent.change(subdir, { target: { value: "invalid/path" } });
  const button = screen.getByText<HTMLButtonElement>("+ New");
  fireEvent.click(button);

  // Error is displayed and button remains enabled
  await screen.findByText(/launch failed/);
  expect(button).toBeEnabled();

  // Editing the subdir clears the error message
  fireEvent.change(subdir, { target: { value: "web" } });
  expect(screen.queryByText(/launch failed/)).not.toBeInTheDocument();
  expect(button).toBeEnabled();

  // Retrying launch succeeds
  fireEvent.click(button);
  await waitFor(() => expect(onLaunched).toHaveBeenCalledTimes(1));
});
