import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { useSessionLauncher } from "../grid/useSessionLauncher";
import type { Server } from "../servers";

const local: Server = { id: "local", name: "local", origin: "https://local.test" };
const remote: Server = { id: "remote", name: "remote", origin: "https://remote.test" };

afterEach(() => vi.restoreAllMocks());

test("starts on the requested server and returns grouped sessions as one ordered batch", async () => {
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const url = String(input);
    if (url.includes("/api/tools")) return new Response(JSON.stringify([{ id: 4, name: "codex", command: "codex" }]));
    if (url.includes("/subdirs")) return new Response("[]");
    if (url.includes("/api/dirs")) return new Response(JSON.stringify([{ id: 7, name: "repo", path: "/repo" }]));
    if (url.includes("/api/sessions") && init?.method === "POST")
      return new Response(
        JSON.stringify([
          { id: 31, tmuxName: "mm-31", toolId: 4, dir: "/repo/web", status: "running" },
          { id: 32, tmuxName: "mm-32", toolId: 4, dir: "/repo/web", status: "running" },
        ]),
        { status: 201 },
      );
    return new Response("[]");
  });

  const { result } = renderHook(() =>
    useSessionLauncher({
      servers: [local, remote],
      initialServerId: "remote",
      targetDir: "/repo/web",
      targetServerId: "remote",
    }),
  );

  await waitFor(() => expect(result.current.canLaunch).toBe(true));
  expect(result.current.server?.id).toBe("remote");
  // The target session's directory, split back into the root it lives under —
  // where the picker opens.
  expect(result.current.start).toEqual({ dirId: 7, subdir: "web" });

  const batch = await act(() => result.current.launch(7, "web"));

  expect(batch?.server.id).toBe("remote");
  expect(batch?.sessions.map((session) => session.id)).toEqual([31, 32]);
});

// A working directory on another daemon, or under a root since removed, has no
// place in this daemon's tree: the picker opens on its landing view instead.
test("a target directory under no configured root leaves the picker at its landing view", async () => {
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const url = String(input);
    if (url.includes("/api/tools")) return new Response(JSON.stringify([{ id: 4, name: "codex", command: "codex" }]));
    if (url.includes("/subdirs")) return new Response("[]");
    if (url.includes("/api/dirs")) return new Response(JSON.stringify([{ id: 7, name: "repo", path: "/repo" }]));
    return new Response("[]");
  });

  const { result } = renderHook(() =>
    useSessionLauncher({ servers: [local], targetDir: "/elsewhere/thing", targetServerId: "local" }),
  );

  await waitFor(() => expect(result.current.canLaunch).toBe(true));
  expect(result.current.start).toBeNull();
});

test("clears per-daemon ids immediately when the server changes", async () => {
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const url = String(input);
    if (url.startsWith(remote.origin)) return await new Promise<Response>(() => undefined);
    if (url.includes("/subdirs")) return new Response("[]");
    if (url.includes("/api/tools")) return new Response(JSON.stringify([{ id: 4, name: "codex", command: "codex" }]));
    if (url.includes("/api/dirs")) return new Response(JSON.stringify([{ id: 7, name: "repo", path: "/repo" }]));
    return new Response("[]");
  });

  const { result } = renderHook(() => useSessionLauncher({ servers: [local, remote] }));
  await waitFor(() => expect(result.current.canLaunch).toBe(true));

  act(() => result.current.selectServer("remote"));
  expect(result.current).toMatchObject({ serverId: "remote", toolId: 0, canLaunch: false });
  expect(result.current.dirs).toEqual([]);
  expect(result.current.recents).toEqual([]);
});

// Recents are gathered per root and merged, so a root's own history must not be
// written over another's when a request lands late.
test("recents merge every root's history, in the roots' order", async () => {
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const url = String(input);
    const history = url.match(/\/api\/dirs\/(\d+)\/subdirs/);
    if (history) return new Response(JSON.stringify(Number(history[1]) === 7 ? ["web/src"] : ["Downloads"]));
    if (url.includes("/api/tools")) return new Response(JSON.stringify([{ id: 4, name: "codex", command: "codex" }]));
    if (url.includes("/api/dirs"))
      return new Response(
        JSON.stringify([
          { id: 7, name: "repo", path: "/repo" },
          { id: 8, name: "home", path: "/home/me" },
        ]),
      );
    return new Response("[]");
  });

  const { result } = renderHook(() => useSessionLauncher({ servers: [local] }));

  await waitFor(() => expect(result.current.recents).toHaveLength(2));
  expect(result.current.recents).toEqual([
    { dirId: 7, subdir: "web/src", path: "/repo/web/src" },
    { dirId: 8, subdir: "Downloads", path: "/home/me/Downloads" },
  ]);
});

test("a stale failed forget cannot restore history the user has moved on from", async () => {
  let releaseDelete!: (response: Response) => void;
  const pendingDelete = new Promise<Response>((resolve) => {
    releaseDelete = resolve;
  });
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const url = String(input);
    const history = url.match(/\/api\/dirs\/(\d+)\/subdirs/);
    if (history) {
      if (init?.method === "DELETE") return pendingDelete;
      if (url.startsWith(remote.origin)) return new Response(JSON.stringify(["Downloads"]));
      return new Response(JSON.stringify(["web/src"]));
    }
    if (url.includes("/api/tools")) return new Response(JSON.stringify([{ id: 4, name: "codex", command: "codex" }]));
    if (url.includes("/api/dirs")) return new Response(JSON.stringify([{ id: 7, name: "repo", path: "/repo" }]));
    return new Response("[]");
  });

  const { result } = renderHook(() => useSessionLauncher({ servers: [local, remote] }));
  await waitFor(() => expect(result.current.recents.map((r) => r.subdir)).toContain("web/src"));

  let forgetting!: Promise<void>;
  act(() => {
    forgetting = result.current.forget({ dirId: 7, subdir: "web/src", path: "/repo/web/src" });
  });
  act(() => result.current.selectServer("remote"));
  releaseDelete(new Response("nope", { status: 500 }));
  await act(async () => forgetting);

  await waitFor(() => expect(result.current.recents.map((r) => r.subdir)).toContain("Downloads"));
  expect(result.current.recents.map((r) => r.subdir)).not.toContain("web/src");
  expect(result.current.error).not.toMatch(/couldn't forget/i);
});

test("a failed launch returns null and becomes retryable", async () => {
  let postCount = 0;
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const url = String(input);
    if (url.includes("/subdirs")) return new Response("[]");
    if (url.includes("/children")) return new Response("[]");
    if (url.includes("/api/tools")) return new Response(JSON.stringify([{ id: 4, name: "codex", command: "codex" }]));
    if (url.includes("/api/dirs")) return new Response(JSON.stringify([{ id: 7, name: "repo", path: "/repo" }]));
    if (url.includes("/api/sessions") && init?.method === "POST") {
      postCount += 1;
      if (postCount === 1) return new Response(JSON.stringify({ error: "directory invalid" }), { status: 400 });
      return new Response(
        JSON.stringify([{ id: 3, tmuxName: "mm-3", toolId: 4, dir: "/repo/web", status: "running" }]),
        { status: 201 },
      );
    }
    return new Response("[]");
  });

  const { result } = renderHook(() => useSessionLauncher({ servers: [local] }));
  await waitFor(() => expect(result.current.canLaunch).toBe(true));

  await act(async () => expect(await result.current.launch(7, "web")).toBeNull());
  expect(result.current.error).toMatch(/^launch failed:/);
  expect(result.current.busy).toBe(false);
  await act(async () => expect((await result.current.launch(7, "web"))?.sessions[0].id).toBe(3));
  expect(result.current.error).toBe("");
});

// The just-launched directory is the most likely next one, so it goes to the
// front of its root's history without waiting for a refetch.
test("a successful launch remembers its subdir", async () => {
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const url = String(input);
    if (url.includes("/subdirs")) return new Response(JSON.stringify(["cmd"]));
    if (url.includes("/api/tools")) return new Response(JSON.stringify([{ id: 4, name: "codex", command: "codex" }]));
    if (url.includes("/api/dirs")) return new Response(JSON.stringify([{ id: 7, name: "repo", path: "/repo" }]));
    if (url.includes("/api/sessions") && init?.method === "POST")
      return new Response(
        JSON.stringify([{ id: 3, tmuxName: "mm-3", toolId: 4, dir: "/repo/web", status: "running" }]),
        { status: 201 },
      );
    return new Response("[]");
  });

  const { result } = renderHook(() => useSessionLauncher({ servers: [local] }));
  await waitFor(() => expect(result.current.canLaunch).toBe(true));

  await act(() => result.current.launch(7, "web"));
  expect(result.current.recents.map((r) => r.subdir)).toEqual(["web", "cmd"]);

  // Launching the same place again keeps it once, still at the front.
  await act(() => result.current.launch(7, "web"));
  expect(result.current.recents.map((r) => r.subdir)).toEqual(["web", "cmd"]);
});

// The root itself has no subdir to remember, so it earns no recents entry.
test("launching a root adds nothing to the recents", async () => {
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const url = String(input);
    if (url.includes("/subdirs")) return new Response("[]");
    if (url.includes("/api/tools")) return new Response(JSON.stringify([{ id: 4, name: "codex", command: "codex" }]));
    if (url.includes("/api/dirs")) return new Response(JSON.stringify([{ id: 7, name: "repo", path: "/repo" }]));
    if (url.includes("/api/sessions") && init?.method === "POST")
      return new Response(JSON.stringify([{ id: 3, tmuxName: "mm-3", toolId: 4, dir: "/repo", status: "running" }]), {
        status: 201,
      });
    return new Response("[]");
  });

  const { result } = renderHook(() => useSessionLauncher({ servers: [local] }));
  await waitFor(() => expect(result.current.canLaunch).toBe(true));

  await act(() => result.current.launch(7, ""));
  expect(result.current.recents).toEqual([]);
});
