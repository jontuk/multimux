import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";
import TerminalTile from "../term/TerminalTile";

vi.mock("@xterm/xterm", () => ({
  Terminal: class {
    cols = 80;
    rows = 24;
    open() {}
    loadAddon() {}
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

class FakeWebSocket {
  static instances: FakeWebSocket[] = [];
  static OPEN = 1;
  url: string;
  binaryType = "";
  readyState = 0;
  onopen: (() => void) | null = null;
  onmessage: ((ev: { data: unknown }) => void) | null = null;
  onclose: (() => void) | null = null;
  constructor(url: string) {
    this.url = url;
    FakeWebSocket.instances.push(this);
  }
  send() {}
  close() {}
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
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      disconnect() {}
    },
  );
});

beforeEach(() => {
  FakeWebSocket.instances = [];
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
