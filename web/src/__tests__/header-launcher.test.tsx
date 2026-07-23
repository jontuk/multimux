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
