import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";
import TerminalTile from "../term/TerminalTile";

const loadedAddons: unknown[] = [];

vi.mock("@xterm/xterm", () => ({
  Terminal: class {
    cols = 80;
    rows = 24;
    open() {}
    loadAddon(addon: unknown) {
      loadedAddons.push(addon);
    }
    onData() {
      return { dispose() {} };
    }
    write() {}
    dispose() {}
  },
}));
vi.mock("@xterm/addon-fit", () => ({
  FitAddon: class {
    fit() {}
  },
}));
vi.mock("@xterm/addon-clipboard", () => ({ ClipboardAddon: class {} }));
vi.mock("@xterm/addon-web-links", () => ({ WebLinksAddon: class WebLinksAddon {} }));

class FakeWebSocket {
  static instances: FakeWebSocket[] = [];
  static OPEN = 1;
  url: string;
  binaryType = "";
  readyState = 0;
  onopen: (() => void) | null = null;
  onmessage: ((ev: { data: unknown }) => void) | null = null;
  onclose: (() => void) | null = null;
  sent: unknown[] = [];
  constructor(url: string) {
    this.url = url;
    FakeWebSocket.instances.push(this);
  }
  send(data: unknown) {
    this.sent.push(data);
  }
  close() {}
}

class FakeResizeObserver {
  static instances: FakeResizeObserver[] = [];
  cb: () => void;
  constructor(cb: () => void) {
    this.cb = cb;
    FakeResizeObserver.instances.push(this);
  }
  observe() {}
  disconnect() {}
}

// Last {"type":"resize"} the tile sent, parsed.
function lastResize(ws: FakeWebSocket) {
  const texts = ws.sent.filter((d): d is string => typeof d === "string").map((d) => JSON.parse(d));
  return texts.filter((m) => m.type === "resize").at(-1);
}

const server = { id: "local", origin: "http://daemon.test", name: "local" };

function mockSessions(response: () => Promise<Response>) {
  return vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    if (String(input).includes("/api/sessions")) return response();
    return new Response("[]");
  });
}

function session(status: string) {
  return { id: 7, tmuxName: "mm-7", toolId: 1, dir: "/a", status };
}

// The first (failed) connection; simulate the handshake being rejected.
// onclose kicks off an async probe, so flush it inside act.
async function failHandshake() {
  const ws = FakeWebSocket.instances[0];
  await act(async () => {
    ws.onclose?.();
  });
}

beforeAll(() => {
  vi.stubGlobal("WebSocket", FakeWebSocket);
  vi.stubGlobal("ResizeObserver", FakeResizeObserver);
});

beforeEach(() => {
  FakeWebSocket.instances = [];
  FakeResizeObserver.instances = [];
});

afterEach(() => {
  vi.restoreAllMocks();
});

test("missing session shows not-found overlay and stops retrying", async () => {
  mockSessions(async () => new Response(JSON.stringify([])));
  const onClose = vi.fn();
  render(<TerminalTile server={server} sessionId={7} onClose={onClose} />);
  await failHandshake();

  await screen.findByText(/session not found/);
  // Past the first backoff (500ms): no reconnect may have been scheduled.
  await new Promise((r) => setTimeout(r, 700));
  expect(FakeWebSocket.instances).toHaveLength(1);

  await userEvent.click(screen.getByRole("button", { name: /dismiss/ }));
  expect(onClose).toHaveBeenCalled();
});

test("dead session shows session-ended overlay and stops retrying", async () => {
  mockSessions(async () => new Response(JSON.stringify([session("dead")])));
  const onClose = vi.fn();
  render(<TerminalTile server={server} sessionId={7} onClose={onClose} />);
  await failHandshake();

  await screen.findByText(/session ended/);
  await new Promise((r) => setTimeout(r, 700));
  expect(FakeWebSocket.instances).toHaveLength(1);

  await userEvent.click(screen.getByRole("button", { name: /dismiss/ }));
  expect(onClose).toHaveBeenCalled();
});

test("unauthorized probe shows auth overlay; reconnect retries", async () => {
  mockSessions(async () => new Response("{}", { status: 401 }));
  render(<TerminalTile server={server} sessionId={7} onClose={() => {}} />);
  await failHandshake();

  await screen.findByText(/not logged in/);
  await new Promise((r) => setTimeout(r, 700));
  expect(FakeWebSocket.instances).toHaveLength(1);

  await userEvent.click(screen.getByRole("button", { name: /reconnect/ }));
  await waitFor(() => expect(FakeWebSocket.instances).toHaveLength(2));
});

test("unreachable daemon keeps retrying with the offline overlay", async () => {
  mockSessions(async () => {
    throw new TypeError("network down");
  });
  render(<TerminalTile server={server} sessionId={7} onClose={() => {}} />);
  await failHandshake();

  await screen.findByText(/daemon unreachable/);
  await waitFor(() => expect(FakeWebSocket.instances.length).toBeGreaterThan(1), { timeout: 2000 });
});

test("running session that closes keeps retrying", async () => {
  mockSessions(async () => new Response(JSON.stringify([session("running")])));
  render(<TerminalTile server={server} sessionId={7} onClose={() => {}} />);
  const ws = FakeWebSocket.instances[0];
  act(() => ws.onopen?.());
  await act(async () => {
    ws.onclose?.();
  });

  await screen.findByText(/daemon unreachable/);
  await waitFor(() => expect(FakeWebSocket.instances.length).toBeGreaterThan(1), { timeout: 2000 });
});

// Window-size ownership is arbitrated server-side and claimed by active:true.
// Only real interaction with THIS terminal may claim it: a reconnecting tile on
// a dormant machine (whose document still reports focus) would otherwise steal
// the shared tmux window and shrink it for whoever is actually typing.
test("only terminal focus claims window-size ownership", async () => {
  mockSessions(async () => new Response(JSON.stringify([session("running")])));
  // The dormant-laptop case: document reports visible and focused throughout.
  vi.spyOn(document, "hasFocus").mockReturnValue(true);
  vi.spyOn(document, "visibilityState", "get").mockReturnValue("visible");

  const { container } = render(<TerminalTile server={server} sessionId={7} onClose={() => {}} />);
  const ws = FakeWebSocket.instances[0];
  ws.readyState = FakeWebSocket.OPEN;

  act(() => ws.onopen?.());
  expect(lastResize(ws)).toMatchObject({ type: "resize", active: false });

  act(() => FakeResizeObserver.instances[0].cb());
  expect(lastResize(ws)).toMatchObject({ active: false });

  act(() => {
    window.dispatchEvent(new Event("focus"));
    document.dispatchEvent(new Event("visibilitychange"));
  });
  expect(lastResize(ws)).toMatchObject({ active: false });

  const term = container.querySelector(".terminal-tile > div")!;
  act(() => {
    term.dispatchEvent(new FocusEvent("focusin", { bubbles: true }));
  });
  expect(lastResize(ws)).toMatchObject({ active: true });
});

// The daemon keys window-size ownership on this id so our own reconnects keep
// the shared tmux window instead of losing it to another machine.
test("PTY socket carries this browser's client id", () => {
  render(<TerminalTile server={server} sessionId={7} onClose={() => {}} />);
  const first = new URL(FakeWebSocket.instances[0].url).searchParams.get("client");
  expect(first).toBeTruthy();

  render(<TerminalTile server={server} sessionId={8} onClose={() => {}} />);
  expect(new URL(FakeWebSocket.instances[1].url).searchParams.get("client")).toBe(first);
});

test("loads WebLinksAddon on terminal mount", () => {
  render(<TerminalTile server={server} sessionId={7} onClose={() => {}} />);
  expect(loadedAddons.some((addon) => addon?.constructor?.name === "WebLinksAddon")).toBe(true);
});
