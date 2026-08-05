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
    // The subdirs check must come before the /api/dirs one — the history path
    // contains it.
    if (url.includes("/subdirs")) return new Response("[]");
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
    if (url.includes("/subdirs")) return new Response("[]");
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
    if (url.includes("/subdirs")) return new Response("[]");
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

const twoDirs = [
  { id: 7, name: "multimux", path: "/repos/multimux" },
  { id: 8, name: "home", path: "/home/jon" },
];

// Mocks a daemon with two dirs and a per-dir history. The subdirs check must
// come before the /api/dirs one — the history path contains it.
function mockDaemonWithHistory(history: Record<number, string[]>) {
  return vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const url = String(input);
    const hist = url.match(/\/api\/dirs\/(\d+)\/subdirs/);
    if (hist) {
      if ((init?.method ?? "GET") === "DELETE") return new Response(null, { status: 204 });
      return new Response(JSON.stringify(history[Number(hist[1])] ?? []));
    }
    if (url.includes("/api/tools")) return new Response(JSON.stringify(localTools));
    if (url.includes("/api/dirs")) return new Response(JSON.stringify(twoDirs));
    if (url.includes("/api/sessions") && (init?.method ?? "GET") === "POST")
      return new Response(JSON.stringify({ id: 3, tmuxName: "mm-3", toolId: 1, dir: "/a", status: "running" }), {
        status: 201,
      });
    return new Response("[]");
  });
}

// A subdir is relative to the selected directory, so it means nothing once the
// directory changes.
test("changing the directory clears the subdir and loads that directory's history", async () => {
  const fetchMock = mockDaemonWithHistory({ 7: ["web/src"], 8: ["Downloads"] });
  render(<HeaderLauncher servers={[servers[0]]} onLaunched={vi.fn()} />);

  const subdir = await screen.findByLabelText<HTMLInputElement>("subdirectory");
  await waitFor(() => expect(fetchMock.mock.calls.some(([u]) => String(u).includes("/api/dirs/7/subdirs"))).toBe(true));
  fireEvent.change(subdir, { target: { value: "web" } });

  fireEvent.change(screen.getByLabelText("dir"), { target: { value: "8" } });

  expect(subdir.value).toBe("");
  await waitFor(() => expect(fetchMock.mock.calls.some(([u]) => String(u).includes("/api/dirs/8/subdirs"))).toBe(true));

  fireEvent.focus(subdir);
  expect(await screen.findByText("Downloads")).toBeInTheDocument();
  expect(screen.queryByText("web/src")).toBeNull();
});

test("the history appears on focus, filters as you type, and hides when nothing matches", async () => {
  mockDaemonWithHistory({ 7: ["web/src", "cmd", "internal/server"] });
  render(<HeaderLauncher servers={[servers[0]]} onLaunched={vi.fn()} />);

  const subdir = await screen.findByLabelText<HTMLInputElement>("subdirectory");
  // Nothing is shown until the field is focused: the header stays quiet.
  await waitFor(() => expect(screen.queryByText("web/src")).toBeNull());

  fireEvent.focus(subdir);
  expect(await screen.findByText("web/src")).toBeInTheDocument();
  expect(screen.getByText("cmd")).toBeInTheDocument();

  // Substring match, so a deep path is reachable without typing its prefix.
  fireEvent.change(subdir, { target: { value: "serv" } });
  expect(screen.getByText("internal/server")).toBeInTheDocument();
  expect(screen.queryByText("cmd")).toBeNull();

  fireEvent.change(subdir, { target: { value: "zzz" } });
  expect(screen.queryByText("internal/server")).toBeNull();

  fireEvent.blur(subdir);
  fireEvent.change(subdir, { target: { value: "" } });
  expect(screen.queryByText("web/src")).toBeNull();
});

test("clicking a remembered subdir fills the field and launches with it", async () => {
  const fetchMock = mockDaemonWithHistory({ 7: ["web/src"] });
  render(<HeaderLauncher servers={[servers[0]]} onLaunched={vi.fn()} />);

  const subdir = await screen.findByLabelText<HTMLInputElement>("subdirectory");
  fireEvent.focus(subdir);
  fireEvent.click(await screen.findByText("web/src"));

  expect(subdir.value).toBe("web/src");
  fireEvent.click(screen.getByText("+ New"));
  await waitFor(() => {
    const post = fetchMock.mock.calls.find(([, init]) => init?.method === "POST");
    expect(post && JSON.parse(String(post[1]?.body)).subdir).toBe("web/src");
  });
});

test("the x forgets a remembered subdir", async () => {
  const fetchMock = mockDaemonWithHistory({ 7: ["web/src", "cmd"] });
  render(<HeaderLauncher servers={[servers[0]]} onLaunched={vi.fn()} />);

  fireEvent.focus(await screen.findByLabelText("subdirectory"));
  await screen.findByText("web/src");
  fireEvent.click(screen.getByLabelText("forget web/src"));

  await waitFor(() => expect(screen.queryByText("web/src")).toBeNull());
  expect(screen.getByText("cmd")).toBeInTheDocument();
  const sent = fetchMock.mock.calls.find(([, init]) => init?.method === "DELETE");
  expect(String(sent?.[0])).toContain(`/api/dirs/7/subdirs?subdir=${encodeURIComponent("web/src")}`);
});

// A failed delete must put the entry back rather than lie about forgetting it.
test("a failed forget restores the entry and reports the error", async () => {
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const url = String(input);
    if (/\/api\/dirs\/\d+\/subdirs/.test(url)) {
      if ((init?.method ?? "GET") === "DELETE") return new Response("nope", { status: 500 });
      return new Response(JSON.stringify(["web/src"]));
    }
    if (url.includes("/api/tools")) return new Response(JSON.stringify(localTools));
    if (url.includes("/api/dirs")) return new Response(JSON.stringify(localDirs));
    return new Response("[]");
  });
  render(<HeaderLauncher servers={[servers[0]]} onLaunched={vi.fn()} />);

  fireEvent.focus(await screen.findByLabelText("subdirectory"));
  fireEvent.click(await screen.findByLabelText("forget web/src"));

  expect(await screen.findByText(/couldn't forget/i)).toBeInTheDocument();
  expect(screen.getByText("web/src")).toBeInTheDocument();
});

// The just-launched subdir is the most likely next one, so it goes to the
// front without waiting for a refetch.
test("a successful launch adds its subdir to the history", async () => {
  mockDaemonWithHistory({ 7: ["cmd"] });
  render(<HeaderLauncher servers={[servers[0]]} onLaunched={vi.fn()} />);

  const subdir = await screen.findByLabelText<HTMLInputElement>("subdirectory");
  fireEvent.change(subdir, { target: { value: "web/src" } });
  fireEvent.click(screen.getByText("+ New"));

  await waitFor(() => expect(subdir.value).toBe("web/src"));
  fireEvent.focus(subdir);
  fireEvent.change(subdir, { target: { value: "" } });
  fireEvent.focus(subdir);
  const rows = await screen.findAllByRole("button", { name: /^(web\/src|cmd)$/ });
  expect(rows.map((r) => r.textContent)).toEqual(["web/src", "cmd"]);
});

// A directory switch while a forget's DELETE is still in flight must not let
// the eventual failure clobber the new directory's history or show an error
// about a subdir that is no longer on screen.
test("switching directories before a failed forget lands leaves the new directory's history alone", async () => {
  let releaseDelete: (() => void) | undefined;
  const deletePending = new Promise<void>((resolve) => {
    releaseDelete = resolve;
  });
  let deleteSettled = false;
  const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const url = String(input);
    const hist = url.match(/\/api\/dirs\/(\d+)\/subdirs/);
    if (hist) {
      if ((init?.method ?? "GET") === "DELETE") {
        await deletePending;
        deleteSettled = true;
        return new Response("nope", { status: 500 });
      }
      const byDir: Record<number, string[]> = { 7: ["web/src"], 8: ["Downloads"] };
      return new Response(JSON.stringify(byDir[Number(hist[1])] ?? []));
    }
    if (url.includes("/api/tools")) return new Response(JSON.stringify(localTools));
    if (url.includes("/api/dirs")) return new Response(JSON.stringify(twoDirs));
    return new Response("[]");
  });
  render(<HeaderLauncher servers={[servers[0]]} onLaunched={vi.fn()} />);

  const subdir = await screen.findByLabelText<HTMLInputElement>("subdirectory");
  await waitFor(() => expect(fetchMock.mock.calls.some(([u]) => String(u).includes("/api/dirs/7/subdirs"))).toBe(true));
  fireEvent.focus(subdir);
  await screen.findByText("web/src");
  fireEvent.click(screen.getByLabelText("forget web/src"));

  // Switch directories while the DELETE for dir 7 is still in flight.
  fireEvent.change(screen.getByLabelText("dir"), { target: { value: "8" } });
  await waitFor(() => expect(fetchMock.mock.calls.some(([u]) => String(u).includes("/api/dirs/8/subdirs"))).toBe(true));
  fireEvent.focus(subdir);
  expect(await screen.findByText("Downloads")).toBeInTheDocument();

  // Let the stale DELETE fail now that the user has moved on.
  releaseDelete?.();
  await waitFor(() => expect(deleteSettled).toBe(true));

  expect(screen.queryByText(/couldn't forget/i)).toBeNull();
  expect(screen.getByText("Downloads")).toBeInTheDocument();
  expect(screen.queryByText("web/src")).toBeNull();
});

test("arrow keys pick a remembered subdir and Enter fills it", async () => {
  const fetchMock = mockDaemonWithHistory({ 7: ["web/src", "cmd"] });
  render(<HeaderLauncher servers={[servers[0]]} onLaunched={vi.fn()} />);

  const subdir = await screen.findByLabelText<HTMLInputElement>("subdirectory");
  fireEvent.focus(subdir);
  await screen.findByText("web/src");

  fireEvent.keyDown(subdir, { key: "ArrowDown" });
  fireEvent.keyDown(subdir, { key: "ArrowDown" });
  fireEvent.keyDown(subdir, { key: "Enter" });

  expect(subdir.value).toBe("cmd");
  // Enter selected rather than launched.
  expect(fetchMock.mock.calls.some(([, init]) => init?.method === "POST")).toBe(false);
});

// Typing must not arm a suggestion: type-and-Enter has always launched what was
// typed, and it still does.
test("Enter after typing launches instead of selecting", async () => {
  const fetchMock = mockDaemonWithHistory({ 7: ["web/src"] });
  render(<HeaderLauncher servers={[servers[0]]} onLaunched={vi.fn()} />);

  const subdir = await screen.findByLabelText<HTMLInputElement>("subdirectory");
  fireEvent.focus(subdir);
  fireEvent.keyDown(subdir, { key: "ArrowDown" });
  fireEvent.change(subdir, { target: { value: "web" } });
  fireEvent.keyDown(subdir, { key: "Enter" });

  await waitFor(() => {
    const post = fetchMock.mock.calls.find(([, init]) => init?.method === "POST");
    expect(post && JSON.parse(String(post[1]?.body)).subdir).toBe("web");
  });
});

test("Escape closes the history without clearing the field", async () => {
  mockDaemonWithHistory({ 7: ["web/src"] });
  render(<HeaderLauncher servers={[servers[0]]} onLaunched={vi.fn()} />);

  const subdir = await screen.findByLabelText<HTMLInputElement>("subdirectory");
  fireEvent.change(subdir, { target: { value: "web" } });
  fireEvent.focus(subdir);
  await screen.findByText("web/src");

  fireEvent.keyDown(subdir, { key: "Escape" });
  expect(screen.queryByText("web/src")).toBeNull();
  expect(subdir.value).toBe("web");
});
