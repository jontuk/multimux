import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import App from "../App";

// Settings keeps the grid mounted but hidden. The terminal is mocked here, so
// these tests see a tile's mount and unmount, not its xterm, WebGL context or
// PTY socket; what a real tile does when its box goes and returns is covered
// in terminal-tile.test.tsx.
const terminalLifecycle = vi.hoisted(() => ({ mounted: vi.fn(), unmounted: vi.fn() }));

vi.mock("../term/TerminalTile", async () => {
  const { useEffect } = await import("react");
  function TerminalTileMock({ sessionId }: { sessionId: number }) {
    useEffect(() => {
      terminalLifecycle.mounted(sessionId);
      return () => terminalLifecycle.unmounted(sessionId);
    }, [sessionId]);
    return <div data-testid={`term-${sessionId}`} />;
  }
  return { default: TerminalTileMock };
});

class FakeWebSocket {
  static instances: FakeWebSocket[] = [];
  onopen: (() => void) | null = null;
  onmessage: ((ev: { data: string }) => void) | null = null;
  onclose: (() => void) | null = null;
  close = vi.fn();
  url: string;
  constructor(url: string) {
    this.url = url;
    FakeWebSocket.instances.push(this);
  }
}

const REMOTE = "https://remote.test:8686";
const health = { status: "ok", setupPending: false, version: "1.0.0" };
const sessions = [
  { id: 1, tmuxName: "mm-1", toolId: 1, dir: "/a", status: "running" },
  { id: 2, tmuxName: "mm-2", toolId: 1, dir: "/b", status: "running" },
];
const layout = {
  shape: { rows: 1, cols: 2 },
  tiles: [
    { serverId: "local", sessionId: 1 },
    { serverId: "local", sessionId: 2 },
  ],
};

let tools = [{ id: 1, name: "claude", command: "claude" }];
let me: () => Response;
let fetchMock: ReturnType<typeof vi.spyOn>;

function gets(path: string, origin = window.location.origin) {
  return fetchMock.mock.calls.filter(([input, init]: [RequestInfo | URL, RequestInit?]) => {
    const url = String(input);
    return url.startsWith(origin) && url.includes(path) && (init?.method ?? "GET") === "GET";
  }).length;
}

function eventSockets(origin = window.location.origin) {
  return FakeWebSocket.instances.filter((ws) => ws.url.startsWith(origin.replace(/^http/, "ws") + "/ws/events"));
}

function go(hash: string) {
  act(() => {
    window.location.hash = hash;
    window.dispatchEvent(new HashChangeEvent("hashchange"));
  });
}

function storeServers(servers: unknown[]) {
  localStorage.setItem("multimux.servers", JSON.stringify(servers));
}

beforeEach(() => {
  FakeWebSocket.instances = [];
  tools = [{ id: 1, name: "claude", command: "claude" }];
  me = () => new Response(JSON.stringify({ name: "jon" }));
  vi.stubGlobal("WebSocket", FakeWebSocket as unknown as typeof WebSocket);
  fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const url = String(input);
    if (url.includes("/healthz")) return new Response(JSON.stringify(health));
    if (url.includes("/api/auth/me")) return me();
    if (url.includes("/api/settings/preferences")) return new Response(JSON.stringify({ confirmTerminate: false }));
    if (url.includes("/api/layout")) return new Response(JSON.stringify(layout));
    if (url.includes("/api/sessions")) return new Response(JSON.stringify(url.startsWith(REMOTE) ? [] : sessions));
    if (url.includes("/api/tools")) return new Response(JSON.stringify(tools));
    if (url.endsWith("/api/dirs")) return new Response(JSON.stringify([{ id: 1, name: "repos", path: "/repos" }]));
    return new Response("[]");
  });
});

afterEach(() => {
  // Unmount before the counters are cleared, or this test's unmounts are
  // counted against the next one.
  cleanup();
  terminalLifecycle.mounted.mockClear();
  terminalLifecycle.unmounted.mockClear();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  localStorage.clear();
  window.history.replaceState({}, "", "/");
});

/** Renders App on the grid route and waits for both terminals. */
async function renderGrid() {
  render(<App />);
  await screen.findByTestId("term-1");
  await screen.findByTestId("term-2");
  await screen.findByText("#1 · claude");
}

test("grid → settings → grid keeps the same grid: no remount, no second initial fetch, sockets left open", async () => {
  await renderGrid();
  const page = document.querySelector(".grid-page") as HTMLElement;
  const term = screen.getByTestId("term-1");
  expect(page).toBeVisible();
  expect(gets("/api/layout")).toBe(1);
  expect(eventSockets()).toHaveLength(1);

  go("#/settings");
  expect(await screen.findByRole("heading", { name: "Settings" })).toBeInTheDocument();

  // Still mounted, but with no box, out of the tab order and out of the
  // accessibility tree: role queries skip what display:none hides.
  expect(document.querySelector(".grid-page")).toBe(page);
  expect(page).toHaveStyle({ display: "none" });
  expect(term).not.toBeVisible();
  expect(screen.queryByRole("button", { name: "terminate session 1" })).toBeNull();
  // The header controls sit outside the hidden page, so they are unmounted.
  expect(screen.queryByRole("button", { name: "+ New" })).toBeNull();
  expect(document.querySelector("#header-controls")).toBeEmptyDOMElement();

  go("#/");
  expect(await screen.findByRole("button", { name: "terminate session 1" })).toBeInTheDocument();
  expect(await screen.findByRole("button", { name: "+ New" })).toBeInTheDocument();
  expect(document.querySelector(".grid-page")).toBe(page);
  expect(screen.getByTestId("term-1")).toBe(term);
  expect(page).toBeVisible();

  expect(terminalLifecycle.mounted).toHaveBeenCalledTimes(2);
  expect(terminalLifecycle.unmounted).not.toHaveBeenCalled();
  expect(gets("/api/layout")).toBe(1);
  expect(eventSockets()).toHaveLength(1);
  expect(eventSockets()[0].close).not.toHaveBeenCalled();
});

test("coming back from settings picks up a changed tool list", async () => {
  await renderGrid();
  const toolFetches = gets("/api/tools");

  go("#/settings");
  await screen.findByRole("heading", { name: "Settings" });
  tools = [{ id: 1, name: "codex", command: "codex" }];

  go("#/");
  // The tile titles read the grid's own tool list; the launcher has its own.
  expect(await screen.findByText("#1 · codex")).toBeInTheDocument();
  expect(await screen.findByRole("option", { name: "codex" })).toBeInTheDocument();
  expect(gets("/api/tools")).toBeGreaterThan(toolFetches);
  expect(terminalLifecycle.unmounted).not.toHaveBeenCalled();
});

test("a server added in settings appears on return, and one removed is torn down", async () => {
  const remote = { id: "r1", origin: REMOTE, name: "remote", token: "t" };
  await renderGrid();
  expect(eventSockets(REMOTE)).toHaveLength(0);

  go("#/settings");
  await screen.findByRole("heading", { name: "Settings" });
  storeServers([remote]);
  go("#/");

  await waitFor(() => expect(eventSockets(REMOTE)).toHaveLength(1));
  await waitFor(() => expect(gets("/api/sessions", REMOTE)).toBe(1));
  expect(gets("/api/tools", REMOTE)).toBe(1);
  // The local daemon's socket and terminals were not disturbed by it.
  expect(eventSockets()).toHaveLength(1);
  expect(eventSockets()[0].close).not.toHaveBeenCalled();
  expect(terminalLifecycle.unmounted).not.toHaveBeenCalled();

  go("#/settings");
  await screen.findByRole("heading", { name: "Settings" });
  storeServers([]);
  go("#/");

  await waitFor(() => expect(eventSockets(REMOTE)[0].close).toHaveBeenCalled());
  expect(eventSockets()[0].close).not.toHaveBeenCalled();
  expect(terminalLifecycle.unmounted).not.toHaveBeenCalled();
});

test("a removed server's tiles lose their terminals on return", async () => {
  storeServers([{ id: "r1", origin: REMOTE, name: "remote", token: "t" }]);
  fetchMock.mockImplementation(async (input: RequestInfo | URL) => {
    const url = String(input);
    if (url.includes("/healthz")) return new Response(JSON.stringify(health));
    if (url.includes("/api/auth/me")) return me();
    if (url.includes("/api/layout"))
      return new Response(
        JSON.stringify({
          shape: { rows: 1, cols: 2 },
          tiles: [
            { serverId: "local", sessionId: 1 },
            { serverId: "r1", sessionId: 9 },
          ],
        }),
      );
    if (url.includes("/api/sessions"))
      return new Response(
        JSON.stringify(
          url.startsWith(REMOTE)
            ? [{ id: 9, tmuxName: "mm-9", toolId: 1, dir: "/r", status: "running" }]
            : [sessions[0]],
        ),
      );
    if (url.includes("/api/tools")) return new Response(JSON.stringify(tools));
    return new Response("[]");
  });
  render(<App />);
  await screen.findByTestId("term-1");
  await screen.findByTestId("term-9");

  go("#/settings");
  await screen.findByRole("heading", { name: "Settings" });
  storeServers([]);
  go("#/");

  expect(await screen.findByText("#9 · server removed")).toBeInTheDocument();
  expect(screen.queryByTestId("term-9")).toBeNull();
  expect(terminalLifecycle.unmounted).toHaveBeenCalledTimes(1);
  expect(terminalLifecycle.unmounted).toHaveBeenCalledWith(9);
  expect(eventSockets(REMOTE)[0].close).toHaveBeenCalled();
  expect(screen.getByTestId("term-1")).toBeInTheDocument();
});

function ctrlAlt(key: string) {
  const event = new KeyboardEvent("keydown", { key, ctrlKey: true, altKey: true, bubbles: true, cancelable: true });
  act(() => {
    document.body.dispatchEvent(event);
  });
  return event;
}

test("the grid's window shortcuts are inert while settings is showing", async () => {
  await renderGrid();
  await screen.findByRole("button", { name: /^a 1/ });

  // On the grid, Ctrl+Alt+→ is taken in the capture phase and solos a directory.
  expect(ctrlAlt("ArrowRight").defaultPrevented).toBe(true);
  await waitFor(() => expect(localStorage.getItem("multimux.soloDir")).toBe(JSON.stringify("/a")));

  // Maximize a tile, so Escape has something to undo.
  fireEvent.doubleClick(screen.getByText("#1 · claude").closest(".tile-header")!);
  expect(document.querySelector(".tile-maximized")).not.toBeNull();

  go("#/settings");
  await screen.findByRole("heading", { name: "Settings" });

  const bubbled = vi.fn();
  window.addEventListener("keydown", bubbled);
  try {
    expect(ctrlAlt("ArrowRight").defaultPrevented).toBe(false);
    expect(ctrlAlt("ArrowLeft").defaultPrevented).toBe(false);
    // Not stopped in the capture phase either: the page under it still hears them.
    expect(bubbled).toHaveBeenCalledTimes(2);
    fireEvent.keyDown(document.body, { key: "Escape" });
  } finally {
    window.removeEventListener("keydown", bubbled);
  }
  expect(localStorage.getItem("multimux.soloDir")).toBe(JSON.stringify("/a"));
  expect(document.querySelector(".tile-maximized")).not.toBeNull();

  // Back on the grid they work again.
  go("#/");
  await screen.findByRole("button", { name: "+ New" });
  fireEvent.keyDown(document.body, { key: "Escape" });
  await waitFor(() => expect(document.querySelector(".tile-maximized")).toBeNull());
  expect(ctrlAlt("ArrowRight").defaultPrevented).toBe(true);
  await waitFor(() => expect(localStorage.getItem("multimux.soloDir")).toBe(JSON.stringify("/b")));
});

test("a hidden grid does not keep focus, and returning focuses nothing in it", async () => {
  await renderGrid();
  const page = document.querySelector(".grid-page") as HTMLElement;
  const inGrid = screen.getByRole("button", { name: "remove session 1 from grid" });
  act(() => inGrid.focus());
  expect(page.contains(document.activeElement)).toBe(true);

  go("#/settings");
  await screen.findByRole("heading", { name: "Settings" });
  await waitFor(() => expect(page.contains(document.activeElement)).toBe(false));

  go("#/");
  await screen.findByRole("button", { name: "+ New" });
  expect(page.contains(document.activeElement)).toBe(false);
});

test("a dialog open on the grid is closed when the grid is hidden", async () => {
  await renderGrid();
  await userEvent.click(screen.getByRole("button", { name: "Read text from session 1" }));
  expect(await screen.findByRole("dialog")).toBeInTheDocument();

  go("#/settings");
  await screen.findByRole("heading", { name: "Settings" });
  go("#/");
  await screen.findByRole("button", { name: "+ New" });

  expect(screen.queryByRole("dialog")).toBeNull();
  expect(terminalLifecycle.unmounted).not.toHaveBeenCalled();
});

test("landing directly on settings does not mount the grid until it is visited", async () => {
  window.location.hash = "#/settings";
  render(<App />);
  expect(await screen.findByRole("heading", { name: "Settings" })).toBeInTheDocument();

  expect(document.querySelector(".grid-page")).toBeNull();
  expect(terminalLifecycle.mounted).not.toHaveBeenCalled();
  expect(gets("/api/layout")).toBe(0);
  expect(gets("/api/sessions")).toBe(0);
  expect(eventSockets()).toHaveLength(1); // the update banner's, not the grid's

  go("#/");
  await screen.findByTestId("term-1");
  await screen.findByTestId("term-2");
  expect(terminalLifecycle.mounted).toHaveBeenCalledTimes(2);
  expect(gets("/api/layout")).toBe(1);
});

test("an unauthenticated start never mounts the grid", async () => {
  me = () => new Response("", { status: 401 });
  render(<App />);
  expect(await screen.findByText("Sign in with passkey")).toBeInTheDocument();

  expect(document.querySelector(".grid-page")).toBeNull();
  expect(terminalLifecycle.mounted).not.toHaveBeenCalled();
  expect(gets("/api/layout")).toBe(0);
  expect(FakeWebSocket.instances).toHaveLength(0);
});

test("only settings keeps the grid: another route unmounts it, and settings does not bring it back", async () => {
  await renderGrid();

  go("#/connect");
  await waitFor(() => expect(document.querySelector(".grid-page")).toBeNull());
  expect(terminalLifecycle.unmounted).toHaveBeenCalledTimes(2);

  go("#/settings");
  await screen.findByRole("heading", { name: "Settings" });
  expect(document.querySelector(".grid-page")).toBeNull();
  expect(terminalLifecycle.mounted).toHaveBeenCalledTimes(2);
});
