import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import App from "../App";

const health = { status: "ok", setupPending: false, version: "1.0.0" };

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

/** Delivers a hello frame to every open events socket. */
function hello(build?: string) {
  const frame = JSON.stringify(build === undefined ? { type: "hello" } : { type: "hello", build });
  for (const ws of FakeWebSocket.instances.filter((w) => w.url.includes("/ws/events"))) {
    ws.onmessage?.({ data: frame });
  }
}

beforeEach(() => {
  FakeWebSocket.instances = [];
  vi.stubGlobal("WebSocket", FakeWebSocket as unknown as typeof WebSocket);
  vi.spyOn(globalThis, "fetch").mockImplementation((input: RequestInfo | URL) => {
    const url = String(input);
    if (url.includes("/healthz")) return Promise.resolve(new Response(JSON.stringify(health)));
    if (url.includes("/api/auth/me")) return Promise.resolve(new Response(JSON.stringify({ name: "jon" })));
    return Promise.resolve(new Response("[]"));
  });
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

/** Renders App and waits for the authenticated shell. */
async function renderApp() {
  render(<App />);
  await screen.findByRole("link", { name: "Grid" });
}

test("a reconnect carrying a different build offers a reload", async () => {
  await renderApp();

  hello("build-one");
  expect(screen.queryByRole("button", { name: "Reload" })).toBeNull();

  hello("build-two");

  expect(await screen.findByRole("button", { name: "Reload" })).toBeInTheDocument();
});

test("reconnecting to the same build stays quiet", async () => {
  await renderApp();

  hello("build-one");
  hello("build-one");

  await waitFor(() => expect(screen.getByRole("link", { name: "Grid" })).toBeInTheDocument());
  expect(screen.queryByRole("button", { name: "Reload" })).toBeNull();
});

test("a daemon that ships no build id never prompts", async () => {
  await renderApp();

  hello();
  hello();

  await waitFor(() => expect(screen.getByRole("link", { name: "Grid" })).toBeInTheDocument());
  expect(screen.queryByRole("button", { name: "Reload" })).toBeNull();
});

test("Reload reloads the page", async () => {
  const reload = vi.fn();
  vi.spyOn(window, "location", "get").mockReturnValue({ ...window.location, reload } as unknown as Location);
  await renderApp();

  hello("build-one");
  hello("build-two");
  await userEvent.click(await screen.findByRole("button", { name: "Reload" }));

  expect(reload).toHaveBeenCalled();
});

test("dismissing hides the banner until a further build arrives", async () => {
  await renderApp();

  hello("build-one");
  hello("build-two");
  await userEvent.click(await screen.findByRole("button", { name: "Dismiss update notice" }));
  expect(screen.queryByRole("button", { name: "Reload" })).toBeNull();

  hello("build-two"); // still the build we dismissed
  expect(screen.queryByRole("button", { name: "Reload" })).toBeNull();

  hello("build-three");
  expect(await screen.findByRole("button", { name: "Reload" })).toBeInTheDocument();
});
