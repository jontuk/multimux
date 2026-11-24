import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createRef } from "react";
import { vi } from "vitest";
import { FitAddon } from "@xterm/addon-fit";
import TerminalTile, { type TerminalHandle } from "../term/TerminalTile";
import { beginReflowHold, endReflowHold, isReflowHeld } from "../term/reflowGate";

const touchScrollMocks = vi.hoisted(() => ({
  cleanup: vi.fn(),
  install: vi.fn(),
}));

vi.mock("../term/touchScroll", () => ({
  installTouchScroll: touchScrollMocks.install,
}));

const loadedAddons: unknown[] = [];
const linkProviders: unknown[] = [];
let dataListener: ((data: string) => void) | null = null;
const pasteCalls: string[] = [];
const focusSpy = vi.fn();
const terminalOptions: Array<{ fontSize?: number }> = [];
type FakeTerminalInstance = {
  cols: number;
  rows: number;
  element: HTMLElement | undefined;
  modes: { mouseTrackingMode: "none" | "any" };
};
const terminalInstances: FakeTerminalInstance[] = [];
// Selection hooks the tile subscribes to; a test drives them via fireSelection.
let selectionListener: (() => void) | null = null;
let selectionText = "";
function fireSelection(text: string) {
  selectionText = text;
  selectionListener?.();
}

vi.mock("@xterm/xterm", () => ({
  Terminal: class {
    cols = 80;
    rows = 24;
    options: { fontSize?: number };
    element: HTMLElement | undefined;
    modes = { mouseTrackingMode: "none" as "none" | "any" };
    constructor(options: { fontSize?: number }) {
      this.options = { ...options };
      terminalOptions.push(this.options);
      terminalInstances.push(this);
    }
    open(parent: HTMLElement) {
      this.element = document.createElement("div");
      this.element.className = "xterm";
      parent.append(this.element);
    }
    loadAddon(addon: unknown) {
      loadedAddons.push(addon);
    }
    onData(cb: (data: string) => void) {
      dataListener = cb;
      return {
        dispose() {
          dataListener = null;
        },
      };
    }
    paste(data: string) {
      pasteCalls.push(data);
      dataListener?.(data);
    }
    focus() {
      focusSpy();
    }
    onSelectionChange(cb: () => void) {
      selectionListener = cb;
      return {
        dispose() {
          selectionListener = null;
        },
      };
    }
    getSelection() {
      return selectionText;
    }
    getSelectionPosition() {
      return undefined; // exercises selectedText's fallback to getSelection
    }
    registerLinkProvider(provider: unknown) {
      linkProviders.push(provider);
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
const webglMocks = vi.hoisted(() => ({
  instances: [] as Array<{ dispose: ReturnType<typeof vi.fn>; loseContext: () => void }>,
  unsupported: false,
  // Called for every addon the tile tries to create, including failed ones.
  construct() {},
}));
vi.mock("@xterm/addon-webgl", () => ({
  WebglAddon: class {
    dispose = vi.fn();
    private lossListener: (() => void) | null = null;
    constructor() {
      webglMocks.construct();
      if (webglMocks.unsupported) throw new Error("WebGL2 not supported");
      webglMocks.instances.push(this);
    }
    onContextLoss(cb: () => void) {
      this.lossListener = cb;
      return { dispose() {} };
    }
    loseContext() {
      this.lossListener?.();
    }
  },
}));

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
  throwOnBinarySend = false;
  constructor(url: string) {
    this.url = url;
    FakeWebSocket.instances.push(this);
  }
  send(data: unknown) {
    if (this.throwOnBinarySend && ArrayBuffer.isView(data)) throw new Error("send failed");
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

function resizeFrames(ws: FakeWebSocket) {
  return ws.sent
    .filter((d): d is string => typeof d === "string")
    .map((d) => JSON.parse(d) as { type: string; cols?: number; rows?: number; active?: boolean })
    .filter((m) => m.type === "resize");
}

function decodedBinaryFrames(ws: FakeWebSocket) {
  const decoder = new TextDecoder();
  return ws.sent
    .filter((data): data is Uint8Array => ArrayBuffer.isView(data) && data.constructor.name === "Uint8Array")
    .map((data) => decoder.decode(data));
}

function sentFrameTypes(ws: FakeWebSocket): string[] {
  return ws.sent.map((data) => {
    if (typeof data !== "string") return "binary";
    return JSON.parse(data).active ? "active-resize" : "resize";
  });
}

// Last {"type":"resize"} the tile sent, parsed.
function lastResize(ws: FakeWebSocket) {
  return resizeFrames(ws).at(-1);
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

// jsdom lays nothing out, so every element measures 0x0 — which is how the
// tile recognises one the dir filter has hidden. Tiles here get a real box
// unless a test takes it away with hideTile.
const tileBox = { width: 800, height: 600 };
const clientSize = {
  width: Object.getOwnPropertyDescriptor(Element.prototype, "clientWidth")!,
  height: Object.getOwnPropertyDescriptor(Element.prototype, "clientHeight")!,
};

// What the browser does when the grid sets display:none on a tile, and when it
// takes it off again: the box goes, or comes back, and the observer reports it.
function hideTile() {
  tileBox.width = tileBox.height = 0;
  act(() => FakeResizeObserver.instances[0].cb());
}
function showTile() {
  tileBox.width = 800;
  tileBox.height = 600;
  act(() => FakeResizeObserver.instances[0].cb());
}

beforeAll(() => {
  vi.stubGlobal("WebSocket", FakeWebSocket);
  vi.stubGlobal("ResizeObserver", FakeResizeObserver);
  Object.defineProperty(Element.prototype, "clientWidth", { configurable: true, get: () => tileBox.width });
  Object.defineProperty(Element.prototype, "clientHeight", { configurable: true, get: () => tileBox.height });
});

afterAll(() => {
  Object.defineProperty(Element.prototype, "clientWidth", clientSize.width);
  Object.defineProperty(Element.prototype, "clientHeight", clientSize.height);
});

beforeEach(() => {
  tileBox.width = 800;
  tileBox.height = 600;
  FakeWebSocket.instances = [];
  FakeResizeObserver.instances = [];
  linkProviders.length = 0;
  webglMocks.instances.length = 0;
  webglMocks.unsupported = false;
  dataListener = null;
  pasteCalls.length = 0;
  focusSpy.mockClear();
  terminalOptions.length = 0;
  terminalInstances.length = 0;
  touchScrollMocks.cleanup.mockReset();
  touchScrollMocks.install.mockReset();
  touchScrollMocks.install.mockReturnValue(touchScrollMocks.cleanup);
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
  // reflowGate is module-level state shared across tiles/tests; never let a
  // failed assertion mid-test leave the gate held for the next test.
  if (isReflowHeld()) endReflowHold();
});

test("touch scrollback is disabled by default", () => {
  const { container } = render(<TerminalTile server={server} sessionId={7} onClose={() => {}} />);

  expect(touchScrollMocks.install).not.toHaveBeenCalled();
  expect(container.querySelector(".touch-scrollback")).toBeNull();
});

test("touch scrollback installs after xterm opens and follows mouse tracking readiness", () => {
  const { container, unmount } = render(
    <TerminalTile server={server} sessionId={7} onClose={() => {}} touchScrollback />,
  );
  const instance = terminalInstances[0];

  expect(container.querySelector(".terminal-tile > .touch-scrollback")).not.toBeNull();
  expect(touchScrollMocks.install).toHaveBeenCalledOnce();
  expect(touchScrollMocks.install).toHaveBeenCalledWith(instance.element, expect.any(Function));
  const isReady = touchScrollMocks.install.mock.calls[0][1] as () => boolean;
  expect(isReady()).toBe(false);
  instance.modes.mouseTrackingMode = "any";
  expect(isReady()).toBe(true);

  unmount();
  expect(touchScrollMocks.cleanup).toHaveBeenCalledOnce();
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

test("tiles on one server that close together share one sessions lookup", async () => {
  let release!: () => void;
  const gate = new Promise<void>((r) => (release = r));
  const fetchMock = mockSessions(async () => {
    await gate;
    return new Response(JSON.stringify([session("dead")]));
  });
  render(
    <>
      <TerminalTile server={server} sessionId={7} onClose={() => {}} />
      <TerminalTile server={server} sessionId={8} onClose={() => {}} />
    </>,
  );
  await act(async () => {
    for (const ws of FakeWebSocket.instances) ws.onclose?.();
  });
  await act(async () => release());

  expect(await screen.findByText(/session ended/)).toBeInTheDocument();
  expect(await screen.findByText(/session not found/)).toBeInTheDocument();
  expect(fetchMock.mock.calls.filter(([input]) => String(input).includes("/api/sessions"))).toHaveLength(1);
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

test("a socket the browser refuses to construct shows the offline overlay and retries on the backoff", async () => {
  // What `new WebSocket` does with a URL it rejects outright: a synchronous
  // throw, where every other failure arrives as a close event.
  let attempts = 0;
  class ThrowingWebSocket {
    static OPEN = 1;
    constructor() {
      attempts++;
      throw new SyntaxError("invalid URL");
    }
  }
  vi.stubGlobal("WebSocket", ThrowingWebSocket);
  try {
    const bad = { id: "bad", origin: "mux.local:8686", name: "bad" };
    const { unmount } = render(<TerminalTile server={bad} sessionId={7} onClose={() => {}} />);

    await screen.findByText(/daemon unreachable/);
    expect(attempts).toBe(1);
    await waitFor(() => expect(attempts).toBe(2), { timeout: 2000 });
    unmount();
  } finally {
    vi.stubGlobal("WebSocket", FakeWebSocket);
  }
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

test("terminal handle sends direct input only while connected", () => {
  const ref = createRef<TerminalHandle>();
  render(<TerminalTile ref={ref} server={server} sessionId={7} onClose={() => {}} />);
  const ws = FakeWebSocket.instances[0];

  expect(ref.current?.input("not sent")).toBe(false);
  expect(decodedBinaryFrames(ws)).toEqual([]);

  ws.readyState = FakeWebSocket.OPEN;
  act(() => ws.onopen?.());

  ws.sent.length = 0;
  expect(ref.current?.input("hello 🌍")).toBe(true);
  expect(decodedBinaryFrames(ws)).toEqual(["hello 🌍"]);
  expect(sentFrameTypes(ws)).toEqual(["active-resize", "binary"]);
});

test("terminal handle pastes multiline Unicode through xterm without adding Enter", () => {
  const ref = createRef<TerminalHandle>();
  render(<TerminalTile ref={ref} server={server} sessionId={7} onClose={() => {}} />);
  const ws = FakeWebSocket.instances[0];
  ws.readyState = FakeWebSocket.OPEN;
  act(() => ws.onopen?.());

  ws.sent.length = 0;
  expect(ref.current?.paste("first\nsecond 🐚")).toBe(true);
  expect(pasteCalls).toEqual(["first\nsecond 🐚"]);
  expect(decodedBinaryFrames(ws)).toEqual(["first\nsecond 🐚"]);
  expect(sentFrameTypes(ws)).toEqual(["active-resize", "binary"]);
});

test("terminal handle reports synchronous input and paste send failures", () => {
  const ref = createRef<TerminalHandle>();
  render(<TerminalTile ref={ref} server={server} sessionId={7} onClose={() => {}} />);
  const ws = FakeWebSocket.instances[0];
  ws.readyState = FakeWebSocket.OPEN;
  act(() => ws.onopen?.());
  ws.throwOnBinarySend = true;

  expect(ref.current?.input("not accepted")).toBe(false);
  expect(ref.current?.paste("also not accepted")).toBe(false);
  expect(decodedBinaryFrames(ws)).toEqual([]);
});

test("terminal handle focuses, changes font size, and fits passively", () => {
  const ref = createRef<TerminalHandle>();
  const fitSpy = vi.spyOn(FitAddon.prototype, "fit");
  const { container } = render(
    <TerminalTile ref={ref} server={server} sessionId={7} onClose={() => {}} sizePolicy="passive" />,
  );
  const ws = FakeWebSocket.instances[0];
  ws.readyState = FakeWebSocket.OPEN;
  const box = container.querySelector(".terminal-tile > div") as HTMLElement;
  Object.defineProperties(box, {
    clientWidth: { configurable: true, value: 390 },
    clientHeight: { configurable: true, value: 600 },
  });
  act(() => ws.onopen?.());

  act(() => {
    ref.current?.focus();
    ref.current?.setFontSize(11);
    ref.current?.fit();
  });

  expect(focusSpy).toHaveBeenCalledOnce();
  expect(terminalOptions[0].fontSize).toBe(11);
  expect(fitSpy).toHaveBeenCalled();
  expect(resizeFrames(ws)).not.toContainEqual(expect.objectContaining({ active: true }));
});

test("terminal handle font size survives an internal reconnect", async () => {
  mockSessions(async () => new Response("{}", { status: 401 }));
  const ref = createRef<TerminalHandle>();
  render(<TerminalTile ref={ref} server={server} sessionId={7} onClose={() => {}} sizePolicy="passive" />);

  act(() => ref.current?.setFontSize(9));
  expect(terminalOptions[0].fontSize).toBe(9);
  await failHandshake();
  await screen.findByText(/not logged in/);

  await userEvent.click(screen.getByRole("button", { name: /reconnect/ }));

  await waitFor(() => expect(terminalOptions).toHaveLength(2));
  expect(terminalOptions[1].fontSize).toBe(9);
});

test("a captured terminal handle becomes inert after unmount", () => {
  const ref = createRef<TerminalHandle>();
  const { unmount } = render(<TerminalTile ref={ref} server={server} sessionId={7} onClose={() => {}} />);
  const handle = ref.current!;
  const ws = FakeWebSocket.instances[0];
  ws.readyState = FakeWebSocket.OPEN;
  act(() => ws.onopen?.());

  unmount();

  expect(ref.current).toBeNull();
  expect(handle.input("stale")).toBe(false);
  expect(handle.paste("stale")).toBe(false);
  expect(decodedBinaryFrames(ws)).toEqual([]);
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
  expect(screen.queryByRole("button", { name: "Fit session to phone" })).not.toBeInTheDocument();
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

test("terminal-generated data is forwarded without claiming size ownership", () => {
  render(<TerminalTile server={server} sessionId={7} onClose={() => {}} />);
  const ws = FakeWebSocket.instances[0];
  ws.readyState = FakeWebSocket.OPEN;
  act(() => ws.onopen?.());

  act(() => dataListener?.("\x1b[?1;2c"));

  expect(decodedBinaryFrames(ws)).toContain("\x1b[?1;2c");
  expect(resizeFrames(ws)).not.toContainEqual(expect.objectContaining({ active: true }));
});

test("deliberate desktop gestures claim size ownership", () => {
  vi.useFakeTimers();
  const { container } = render(<TerminalTile server={server} sessionId={7} onClose={() => {}} />);
  const ws = FakeWebSocket.instances[0];
  ws.readyState = FakeWebSocket.OPEN;
  act(() => ws.onopen?.());
  const term = container.querySelector(".terminal-tile > div")!;

  for (const event of [
    new KeyboardEvent("keydown", { bubbles: true, key: "a" }),
    new Event("paste", { bubbles: true }),
    new MouseEvent("pointerdown", { bubbles: true, button: 0 }),
    new WheelEvent("wheel", { bubbles: true, deltaY: 1 }),
  ]) {
    ws.sent.length = 0;
    act(() => term.dispatchEvent(event));
    expect(resizeFrames(ws)).toEqual([expect.objectContaining({ active: true })]);
    vi.advanceTimersByTime(1000); // past the repeat interval: each is a first gesture
  }
});

function openTile() {
  const view = render(<TerminalTile server={server} sessionId={7} onClose={() => {}} />);
  const ws = FakeWebSocket.instances[0];
  ws.readyState = FakeWebSocket.OPEN;
  act(() => ws.onopen?.());
  ws.sent.length = 0;
  return { ws, term: view.container.querySelector(".terminal-tile > div")! };
}

const keydown = () => new KeyboardEvent("keydown", { bubbles: true, key: "a" });
const wheel = () => new WheelEvent("wheel", { bubbles: true, deltaY: 1 });
const activeResizes = (ws: FakeWebSocket) => resizeFrames(ws).filter((m) => m.active);

// A held key or a trackpad scroll is dozens of events a second, and every claim
// costs the daemon a frame to decode and two locks. One claim says it all until
// something changes.
test("repeated gestures at an unchanged size send one claim per interval", () => {
  vi.useFakeTimers();
  const { ws, term } = openTile();

  act(() => {
    for (let i = 0; i < 50; i++) {
      term.dispatchEvent(i % 2 ? keydown() : wheel());
      vi.advanceTimersByTime(10);
    }
  });
  expect(activeResizes(ws)).toHaveLength(1);

  // The daemon's presence record is renewed by the claims that do go out, so
  // sustained interaction must keep producing them.
  act(() => {
    vi.advanceTimersByTime(500); // 1000ms since the claim
    term.dispatchEvent(keydown());
    term.dispatchEvent(keydown());
  });
  expect(activeResizes(ws)).toHaveLength(2);
});

test("input is never held back with its claim", () => {
  vi.useFakeTimers();
  const ref = createRef<TerminalHandle>();
  render(<TerminalTile ref={ref} server={server} sessionId={7} onClose={() => {}} />);
  const ws = FakeWebSocket.instances[0];
  ws.readyState = FakeWebSocket.OPEN;
  act(() => ws.onopen?.());
  ws.sent.length = 0;

  expect(ref.current?.input("a")).toBe(true);
  expect(ref.current?.input("b")).toBe(true);
  expect(sentFrameTypes(ws)).toEqual(["active-resize", "binary", "binary"]);
});

test("a gesture at a new size claims at once", () => {
  vi.useFakeTimers();
  const { ws, term } = openTile();

  act(() => term.dispatchEvent(keydown()));
  terminalInstances[0].cols = 100;
  act(() => term.dispatchEvent(keydown()));
  terminalInstances[0].rows = 30;
  act(() => term.dispatchEvent(keydown()));
  act(() => term.dispatchEvent(keydown()));

  expect(activeResizes(ws)).toEqual([
    expect.objectContaining({ cols: 80, rows: 24 }),
    expect.objectContaining({ cols: 100, rows: 24 }),
    expect.objectContaining({ cols: 100, rows: 30 }),
  ]);
});

test("a reflow, a blur, or the tab losing focus makes the next gesture claim at once", () => {
  vi.useFakeTimers();
  const { ws, term } = openTile();
  act(() => term.dispatchEvent(keydown()));

  const interruptions = [
    () => FakeResizeObserver.instances[0].cb(), // box changed, even if the dims did not
    () => term.dispatchEvent(new FocusEvent("focusout", { bubbles: true })),
    () => window.dispatchEvent(new Event("blur")),
    () => window.dispatchEvent(new Event("focus")),
    () => document.dispatchEvent(new Event("visibilitychange")),
  ];
  interruptions.forEach((interrupt, i) => {
    act(() => {
      interrupt();
      term.dispatchEvent(keydown());
      term.dispatchEvent(keydown());
    });
    expect(activeResizes(ws)).toHaveLength(i + 2);
  });
});

// The daemon's record of the claim died with the socket that carried it.
test("the first gesture on a reconnected socket claims at once", async () => {
  vi.useFakeTimers();
  mockSessions(async () => new Response(JSON.stringify([session("running")])));
  const { ws, term } = openTile();
  act(() => term.dispatchEvent(keydown()));
  expect(activeResizes(ws)).toHaveLength(1);

  ws.readyState = 3;
  await act(async () => {
    ws.onclose?.();
    await vi.advanceTimersByTimeAsync(500); // the first reconnect backoff
  });
  expect(FakeWebSocket.instances).toHaveLength(2);
  const next = FakeWebSocket.instances[1];
  // Still connecting: nothing can be sent, and nothing may be counted as sent.
  act(() => term.dispatchEvent(keydown()));
  next.readyState = FakeWebSocket.OPEN;
  act(() => next.onopen?.());
  expect(activeResizes(next)).toHaveLength(0);

  act(() => {
    term.dispatchEvent(keydown());
    term.dispatchEvent(keydown());
  });
  expect(activeResizes(next)).toHaveLength(1);
  expect(activeResizes(ws)).toHaveLength(1);
});

// Date.now can step backwards (NTP, a manual change). That must not read as
// "claimed in the future" and silence this tile until the clock catches up.
test("a clock stepped backwards does not suppress claims", () => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date("2026-01-01T12:00:00Z"));
  const { ws, term } = openTile();
  act(() => term.dispatchEvent(keydown()));

  vi.setSystemTime(new Date("2026-01-01T11:00:00Z"));
  act(() => term.dispatchEvent(keydown()));
  expect(activeResizes(ws)).toHaveLength(2);
});

// A machine waking with the tab frontmost re-fires focus on the element that
// already had it. That is not a person asking for the window.
test("a focus the user did not cause does not claim the shared size", () => {
  const activation = { isActive: false, hasBeenActive: true };
  Object.defineProperty(navigator, "userActivation", { value: activation, configurable: true });
  try {
    const { container } = render(<TerminalTile server={server} sessionId={7} onClose={() => {}} />);
    const ws = FakeWebSocket.instances[0];
    ws.readyState = FakeWebSocket.OPEN;
    act(() => ws.onopen?.());

    const term = container.querySelector(".terminal-tile > div")!;
    act(() => term.dispatchEvent(new FocusEvent("focusin", { bubbles: true })));
    expect(resizeFrames(ws)).not.toContainEqual(expect.objectContaining({ active: true }));

    activation.isActive = true; // the user clicked into the tile
    act(() => term.dispatchEvent(new FocusEvent("focusin", { bubbles: true })));
    expect(lastResize(ws)).toMatchObject({ active: true });
  } finally {
    Reflect.deleteProperty(navigator, "userActivation");
  }
});

test("passive terminal advertises its size policy and focus does not claim", () => {
  const { container } = render(<TerminalTile server={server} sessionId={7} onClose={() => {}} sizePolicy="passive" />);
  const fitButton = screen.getByRole("button", { name: "Fit session to phone" });
  expect(fitButton).toHaveTextContent(/^Fit$/);
  expect(fitButton).toBeDisabled();

  const ws = FakeWebSocket.instances[0];
  expect(new URL(ws.url).searchParams.get("size")).toBe("passive");
  ws.readyState = FakeWebSocket.OPEN;
  act(() => ws.onopen?.());
  expect(fitButton).toBeEnabled();

  const term = container.querySelector(".terminal-tile > div")!;
  act(() => term.dispatchEvent(new FocusEvent("focusin", { bubbles: true })));

  expect(resizeFrames(ws)).not.toContainEqual(expect.objectContaining({ active: true }));
});

test("passive terminal interaction never claims size ownership", () => {
  const ref = createRef<TerminalHandle>();
  const { container } = render(
    <TerminalTile ref={ref} server={server} sessionId={7} onClose={() => {}} sizePolicy="passive" />,
  );
  const ws = FakeWebSocket.instances[0];
  ws.readyState = FakeWebSocket.OPEN;
  act(() => ws.onopen?.());
  const term = container.querySelector(".terminal-tile > div")!;

  act(() => {
    term.dispatchEvent(new KeyboardEvent("keydown", { bubbles: true, key: "a" }));
    term.dispatchEvent(new Event("paste", { bubbles: true }));
    term.dispatchEvent(new MouseEvent("pointerdown", { bubbles: true, button: 0 }));
    term.dispatchEvent(new WheelEvent("wheel", { bubbles: true, deltaY: 1 }));
    ref.current?.input("a");
    ref.current?.paste("pasted");
  });

  expect(resizeFrames(ws)).not.toContainEqual(expect.objectContaining({ active: true }));
});

test("a passive terminal portals Fit into the supplied controls target", () => {
  const controls = document.createElement("div");
  controls.className = "mobile-terminal-controls";
  document.body.append(controls);
  const { container, unmount } = render(
    <TerminalTile server={server} sessionId={7} onClose={() => {}} sizePolicy="passive" controlsSlot={controls} />,
  );

  expect(controls).toContainElement(screen.getByRole("button", { name: "Fit session to phone" }));
  expect(container.querySelector(".fit-session-button")).toBeNull();

  unmount();
  expect(controls).toBeEmptyDOMElement();
  controls.remove();
});

test("phone fit sends one active resize and later reflow stays passive", async () => {
  render(<TerminalTile server={server} sessionId={7} onClose={() => {}} sizePolicy="passive" />);
  const ws = FakeWebSocket.instances[0];
  ws.readyState = FakeWebSocket.OPEN;
  act(() => ws.onopen?.());

  await userEvent.click(screen.getByRole("button", { name: "Fit session to phone" }));
  expect(resizeFrames(ws).filter((m) => m.active)).toHaveLength(1);

  act(() => FakeResizeObserver.instances[0].cb());
  expect(resizeFrames(ws).at(-1)).toMatchObject({ active: false });
  expect(resizeFrames(ws).filter((m) => m.active)).toHaveLength(1);
});

test("resize observations are suppressed while a splitter drag holds the gate", async () => {
  mockSessions(async () => new Response(JSON.stringify([session("running")])));
  render(<TerminalTile server={server} sessionId={7} onClose={() => {}} />);
  const ws = FakeWebSocket.instances[0];
  ws.readyState = FakeWebSocket.OPEN;
  act(() => ws.onopen?.());

  // Frames the tile sent so far, resize frames only.
  const sentFrames = () =>
    ws.sent
      .filter((d): d is string => typeof d === "string")
      .map((d) => JSON.parse(d))
      .filter((m) => m.type === "resize");

  const before = sentFrames().length;
  beginReflowHold();
  act(() => FakeResizeObserver.instances[0].cb());
  act(() => FakeResizeObserver.instances[0].cb());
  expect(sentFrames().length).toBe(before);

  act(() => endReflowHold());
  // One catch-up resize for the tile, not one per observation.
  expect(sentFrames().length).toBe(before + 1);
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

// tmux mouse mode owns click-drag, so xterm.js only selects when its
// force-selection modifier is set on the mousedown (shiftKey off Mac, altKey
// on it). The tile drives that flag off the inverse of Shift: plain drag
// selects, Shift+drag reaches tmux.
test("plain drag forces selection, Shift+drag passes through to tmux", () => {
  const { container } = render(<TerminalTile server={server} sessionId={7} onClose={() => {}} />);
  const term = container.querySelector(".terminal-tile > div")!;
  // jsdom is not a Mac, so the flag under test is shiftKey.
  const forced = (init: MouseEventInit) => {
    const e = new MouseEvent("mousedown", { bubbles: true, button: 0, ...init });
    term.dispatchEvent(e);
    return e.shiftKey;
  };

  expect(forced({})).toBe(true);
  expect(forced({ shiftKey: true })).toBe(false);
  // Non-primary buttons are left alone: right-click must keep its menu.
  const right = new MouseEvent("mousedown", { bubbles: true, button: 2 });
  term.dispatchEvent(right);
  expect(right.shiftKey).toBe(false);
});

test("selection is copied to the clipboard once the drag settles", async () => {
  const writeText = vi.fn().mockResolvedValue(undefined);
  // stubGlobal is off-limits here: unstubbing would also drop the WebSocket and
  // ResizeObserver stubs the whole file installs in beforeAll.
  Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
  render(<TerminalTile server={server} sessionId={7} onClose={() => {}} />);

  // Mid-drag churn must not race the final write.
  act(() => fireSelection("hel"));
  act(() => fireSelection("hello"));
  await waitFor(() => expect(writeText).toHaveBeenCalledTimes(1));
  expect(writeText).toHaveBeenCalledWith("hello");

  // Clearing a selection must not wipe the clipboard.
  act(() => fireSelection(""));
  await new Promise((r) => setTimeout(r, 250));
  expect(writeText).toHaveBeenCalledTimes(1);
  Reflect.deleteProperty(navigator, "clipboard");
});

test("registers the wrap-aware link provider on terminal mount", () => {
  render(<TerminalTile server={server} sessionId={7} onClose={() => {}} />);
  expect(linkProviders).toHaveLength(1);
});

test("renders through WebGL, and asks for a new context when the browser takes its own", () => {
  vi.useFakeTimers();
  render(<TerminalTile server={server} sessionId={7} onClose={() => {}} />);
  expect(webglMocks.instances).toHaveLength(1);
  const webgl = webglMocks.instances[0];
  expect(loadedAddons).toContain(webgl);
  expect(webgl.dispose).not.toHaveBeenCalled();

  // Past the browser's context cap, or after a GPU reset: dispose the addon so
  // xterm renders through the DOM again instead of leaving a blank tile...
  webgl.loseContext();
  expect(webgl.dispose).toHaveBeenCalled();
  expect(webglMocks.instances).toHaveLength(1);

  // ...and then try again, with a new addon: a disposed one cannot be reused.
  vi.advanceTimersByTime(1000);
  expect(webglMocks.instances).toHaveLength(2);
  expect(loadedAddons).toContain(webglMocks.instances[1]);
  expect(webglMocks.instances[1].dispose).not.toHaveBeenCalled();
});

test("a tile that keeps losing its context settles on the DOM renderer", () => {
  vi.useFakeTimers();
  render(<TerminalTile server={server} sessionId={7} onClose={() => {}} />);

  // Three contexts lost in quick succession are each replaced...
  for (let i = 0; i < 3; i++) {
    expect(webglMocks.instances).toHaveLength(i + 1);
    webglMocks.instances[i].loseContext();
    vi.advanceTimersByTime(1000);
  }
  // ...but the third loss is the last: no fourth context, however long it waits
  // and whatever happens to its box.
  expect(webglMocks.instances).toHaveLength(3);
  vi.advanceTimersByTime(600_000);
  hideTile();
  showTile();
  expect(webglMocks.instances).toHaveLength(3);
});

test("a context that lasted does not count against the tile when it is lost", () => {
  vi.useFakeTimers();
  render(<TerminalTile server={server} sessionId={7} onClose={() => {}} />);

  // A GPU reset every so often, over a long-lived tile, is always recovered.
  for (let i = 0; i < 6; i++) {
    vi.advanceTimersByTime(60_000);
    webglMocks.instances[i].loseContext();
    vi.advanceTimersByTime(1000);
    expect(webglMocks.instances).toHaveLength(i + 2);
  }
});

test("a context lost by a hidden tile is not replaced until the tile is shown", () => {
  vi.useFakeTimers();
  render(<TerminalTile server={server} sessionId={7} onClose={() => {}} />);
  webglMocks.instances[0].loseContext();
  hideTile();
  vi.advanceTimersByTime(1000);
  expect(webglMocks.instances).toHaveLength(1);

  showTile();
  expect(webglMocks.instances).toHaveLength(2);
});

// Browsers cap live WebGL contexts (Chrome at 16). A tile the dir filter has
// hidden draws nothing, so it gives its context up rather than costing a
// visible tile one.
test("a hidden tile releases its WebGL context and takes a new one when shown", () => {
  render(<TerminalTile server={server} sessionId={7} onClose={() => {}} />);
  expect(webglMocks.instances).toHaveLength(1);

  // The canvas the real addon draws on, so the release can be seen to give the
  // context itself back rather than leaving it to the garbage collector.
  const loseContext = vi.fn();
  const canvas = document.createElement("canvas");
  canvas.getContext = vi.fn(() => ({
    getExtension: (name: string) => (name === "WEBGL_lose_context" ? { loseContext } : null),
  })) as unknown as HTMLCanvasElement["getContext"];
  terminalInstances[0].element!.append(canvas);

  hideTile();
  expect(webglMocks.instances[0].dispose).toHaveBeenCalledTimes(1);
  expect(canvas.getContext).toHaveBeenCalledWith("webgl2");
  expect(loseContext).toHaveBeenCalledTimes(1);
  expect(webglMocks.instances).toHaveLength(1);

  // Staying hidden is not a reason to do any of it again.
  act(() => FakeResizeObserver.instances[0].cb());
  expect(webglMocks.instances[0].dispose).toHaveBeenCalledTimes(1);
  expect(webglMocks.instances).toHaveLength(1);

  showTile();
  expect(webglMocks.instances).toHaveLength(2);
  expect(loadedAddons).toContain(webglMocks.instances[1]);
  expect(webglMocks.instances[1].dispose).not.toHaveBeenCalled();

  // Nor is staying visible.
  act(() => FakeResizeObserver.instances[0].cb());
  expect(webglMocks.instances).toHaveLength(2);
});

test("a tile mounted hidden takes no WebGL context until it is shown", () => {
  tileBox.width = tileBox.height = 0;
  render(<TerminalTile server={server} sessionId={7} onClose={() => {}} />);
  expect(webglMocks.instances).toHaveLength(0);
  expect(FakeWebSocket.instances).toHaveLength(1);

  showTile();
  expect(webglMocks.instances).toHaveLength(1);
});

test("hiding and showing a tile keeps its terminal, its socket, and its output", () => {
  const writeSpy = vi.fn();
  render(<TerminalTile server={server} sessionId={7} onClose={() => {}} />);
  const ws = FakeWebSocket.instances[0];
  const closeSpy = vi.spyOn(ws, "close");
  (terminalInstances[0] as unknown as { write: typeof writeSpy }).write = writeSpy;
  ws.readyState = FakeWebSocket.OPEN;
  act(() => ws.onopen?.());

  hideTile();
  // Output for a hidden tile still reaches xterm's buffer.
  act(() => ws.onmessage?.({ data: new Uint8Array([104, 105]).buffer }));
  expect(writeSpy).toHaveBeenCalledTimes(1);
  showTile();

  expect(terminalInstances).toHaveLength(1);
  expect(FakeWebSocket.instances).toHaveLength(1);
  expect(closeSpy).not.toHaveBeenCalled();
});

// Every resize frame reaches the Arbiter, so swapping renderers must not add
// one: the only frames across a hide and a show are the passive reflows the
// ResizeObserver already sent before the renderer was involved.
test("swapping renderers sends no resize of its own", () => {
  vi.useFakeTimers();
  render(<TerminalTile server={server} sessionId={7} onClose={() => {}} />);
  const ws = FakeWebSocket.instances[0];
  ws.readyState = FakeWebSocket.OPEN;
  act(() => ws.onopen?.());
  expect(sentFrameTypes(ws)).toEqual(["resize"]);

  // One observation each way, one passive frame each: what a tile with no
  // renderer to swap sends too (see the splitter-drag test above).
  hideTile();
  expect(sentFrameTypes(ws)).toEqual(["resize", "resize"]);
  showTile();
  expect(sentFrameTypes(ws)).toEqual(["resize", "resize", "resize"]);
  expect(webglMocks.instances).toHaveLength(2);

  // A context lost and re-acquired with no observation at all sends nothing.
  webglMocks.instances[1].loseContext();
  vi.advanceTimersByTime(1000);
  expect(webglMocks.instances).toHaveLength(3);
  expect(sentFrameTypes(ws)).toEqual(["resize", "resize", "resize"]);
});

test("the renderer is released even while a splitter drag holds reflows", () => {
  render(<TerminalTile server={server} sessionId={7} onClose={() => {}} />);
  beginReflowHold();
  hideTile();
  expect(webglMocks.instances[0].dispose).toHaveBeenCalledTimes(1);
});

test("a browser without WebGL2 stops asking for it", () => {
  webglMocks.unsupported = true;
  const construct = vi.spyOn(webglMocks, "construct");
  render(<TerminalTile server={server} sessionId={7} onClose={() => {}} />);
  for (let i = 0; i < 5; i++) {
    hideTile();
    showTile();
  }
  expect(construct).toHaveBeenCalledTimes(3);
});

test("a browser without WebGL2 still gets a working terminal", () => {
  webglMocks.unsupported = true;
  render(<TerminalTile server={server} sessionId={7} onClose={() => {}} />);
  expect(terminalInstances).toHaveLength(1);
  expect(FakeWebSocket.instances).toHaveLength(1);
});

// A tile the dir filter has hidden has no box at all. Fitting to it would size
// the terminal — and this connection's PTY — to nothing, and that size would
// still be there when the tile came back.
test("a tile with no box does not reflow", async () => {
  mockSessions(async () => new Response(JSON.stringify([session("running")])));
  const fitSpy = vi.spyOn(FitAddon.prototype, "fit");
  const { container } = render(<TerminalTile server={server} sessionId={7} onClose={() => {}} />);
  const ws = FakeWebSocket.instances[0];
  ws.readyState = FakeWebSocket.OPEN;
  const box = container.querySelector(".terminal-tile > div") as HTMLElement;

  const setBox = (w: number, h: number) => {
    Object.defineProperty(box, "clientWidth", { configurable: true, value: w });
    Object.defineProperty(box, "clientHeight", { configurable: true, value: h });
  };

  setBox(0, 0);
  act(() => ws.onopen?.());
  act(() => FakeResizeObserver.instances[0].cb());
  expect(fitSpy).not.toHaveBeenCalled();

  // Back on screen: the observer fires with a real box and the terminal refits.
  setBox(800, 600);
  act(() => FakeResizeObserver.instances[0].cb());
  expect(fitSpy).toHaveBeenCalled();
});

function imagePasteEvent(files: File[]) {
  const ev = new Event("paste", { bubbles: true, cancelable: true });
  Object.defineProperty(ev, "clipboardData", { value: { files, items: [], types: ["Files"] } });
  return ev;
}

test("pasting an image uploads it to the tile's daemon and pastes the saved path", async () => {
  const fetchSpy = vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    if (String(input).endsWith("/api/uploads")) {
      return new Response(JSON.stringify({ path: "/data/uploads/paste 1.png" }), { status: 201 });
    }
    return new Response("[]");
  });
  render(<TerminalTile server={server} sessionId={7} onClose={() => {}} />);
  const ws = FakeWebSocket.instances[0];
  ws.readyState = FakeWebSocket.OPEN;
  act(() => ws.onopen?.());
  const target = terminalInstances[0].element!;

  const ev = imagePasteEvent([new File(["x"], "s.png", { type: "image/png" })]);
  act(() => {
    target.dispatchEvent(ev);
  });
  expect(ev.defaultPrevented).toBe(true);
  await waitFor(() => expect(pasteCalls).toEqual(["/data/uploads/paste\\ 1.png"]));
  expect(fetchSpy).toHaveBeenCalledWith("http://daemon.test/api/uploads", expect.objectContaining({ method: "POST" }));
  expect(screen.queryByRole("status")).toBeNull();
});

test("a failed image upload shows the error and pastes nothing", async () => {
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    if (String(input).endsWith("/api/uploads")) {
      return new Response(JSON.stringify({ error: "uploads disabled" }), { status: 503 });
    }
    return new Response("[]");
  });
  render(<TerminalTile server={server} sessionId={7} onClose={() => {}} />);
  const ws = FakeWebSocket.instances[0];
  ws.readyState = FakeWebSocket.OPEN;
  act(() => ws.onopen?.());

  act(() => {
    terminalInstances[0].element!.dispatchEvent(imagePasteEvent([new File(["x"], "s.png", { type: "image/png" })]));
  });
  expect(await screen.findByText(/image upload failed.*uploads disabled/)).toBeInTheDocument();
  expect(pasteCalls).toEqual([]);
});

test("a text paste is left to xterm", () => {
  const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response("[]"));
  render(<TerminalTile server={server} sessionId={7} onClose={() => {}} />);
  const ev = new Event("paste", { bubbles: true, cancelable: true });
  Object.defineProperty(ev, "clipboardData", { value: { files: [], items: [], types: ["text/plain"] } });
  act(() => {
    terminalInstances[0].element!.dispatchEvent(ev);
  });
  expect(ev.defaultPrevented).toBe(false);
  expect(fetchSpy).not.toHaveBeenCalledWith(expect.stringContaining("/api/uploads"), expect.anything());
});

function fileDropEvent(files: File[]) {
  const ev = new Event("drop", { bubbles: true, cancelable: true });
  Object.defineProperty(ev, "dataTransfer", { value: { files, items: [], types: ["Files"] } });
  return ev;
}

test("dropping an image focuses the receiving terminal before uploading", async () => {
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    if (String(input).endsWith("/api/uploads")) {
      return new Response(JSON.stringify({ path: "/data/uploads/paste-1.png" }), { status: 201 });
    }
    return new Response("[]");
  });
  render(<TerminalTile server={server} sessionId={7} onClose={() => {}} />);
  const ws = FakeWebSocket.instances[0];
  ws.readyState = FakeWebSocket.OPEN;
  act(() => ws.onopen?.());
  focusSpy.mockClear();

  const ev = fileDropEvent([new File(["x"], "s.png", { type: "image/png" })]);
  act(() => {
    terminalInstances[0].element!.dispatchEvent(ev);
  });
  // Synchronously, so keys typed while the upload runs go to this session.
  expect(focusSpy).toHaveBeenCalledOnce();
  expect(ev.defaultPrevented).toBe(true);
  await waitFor(() => expect(pasteCalls).toEqual(["/data/uploads/paste-1.png"]));
});

test("a rejected drop does not move focus", () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response("[]"));
  render(<TerminalTile server={server} sessionId={7} onClose={() => {}} />);
  focusSpy.mockClear();

  act(() => {
    terminalInstances[0].element!.dispatchEvent(fileDropEvent([new File(["x"], "a.pdf", { type: "application/pdf" })]));
  });
  expect(focusSpy).not.toHaveBeenCalled();
  expect(screen.getByRole("status")).toHaveTextContent(/only png, jpeg, gif, or webp/);
});
