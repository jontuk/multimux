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
  expect(result.current.dirId).toBe(7);
  expect(result.current.subdir).toBe("web");

  const batch = await act(() => result.current.launch());

  expect(batch?.server.id).toBe("remote");
  expect(batch?.sessions.map((session) => session.id)).toEqual([31, 32]);
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
  expect(result.current).toMatchObject({
    serverId: "remote",
    toolId: 0,
    dirId: 0,
    subdir: "",
    canLaunch: false,
  });
});

test("a stale failed forget cannot restore another directory's history", async () => {
  let releaseDelete!: (response: Response) => void;
  const pendingDelete = new Promise<Response>((resolve) => {
    releaseDelete = resolve;
  });
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const url = String(input);
    const history = url.match(/\/api\/dirs\/(\d+)\/subdirs/);
    if (history) {
      if (init?.method === "DELETE") return pendingDelete;
      return new Response(JSON.stringify(Number(history[1]) === 7 ? ["web/src"] : ["Downloads"]));
    }
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
  await waitFor(() => expect(result.current.options.map((option) => option.value)).toContain("web/src"));

  let forgetting!: Promise<void>;
  act(() => {
    forgetting = result.current.forget("web/src");
  });
  act(() => result.current.selectDir(8));
  releaseDelete(new Response("nope", { status: 500 }));
  await act(async () => forgetting);

  await waitFor(() => expect(result.current.options.map((option) => option.value)).toContain("Downloads"));
  expect(result.current.options.map((option) => option.value)).not.toContain("web/src");
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

  await act(async () => expect(await result.current.launch()).toBeNull());
  expect(result.current.error).toMatch(/^launch failed:/);
  expect(result.current.busy).toBe(false);
  act(() => result.current.changeSubdir("web"));
  expect(result.current.error).toBe("");
  await act(async () => expect((await result.current.launch())?.sessions[0].id).toBe(3));
});
