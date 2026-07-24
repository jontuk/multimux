import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, vi } from "vitest";
import App from "../App";
import PreferencesPanel, { PREFERENCES_EVENT } from "../settings/PreferencesPanel";

// The App test below drives the real GridPage; stub its live pieces.
vi.mock("../useEvents", () => ({ useEvents: vi.fn() }));
vi.mock("../term/TerminalTile", () => ({
  default: ({ sessionId }: { sessionId: number }) => <div data-testid={`term-${sessionId}`} />,
}));

afterEach(() => {
  vi.restoreAllMocks();
});

test("preferences panel loads, saves, and dispatches the update event", async () => {
  const fetchMock = vi
    .spyOn(globalThis, "fetch")
    .mockResolvedValueOnce(new Response(JSON.stringify({ confirmTerminate: true })))
    .mockResolvedValueOnce(new Response(JSON.stringify({ confirmTerminate: false })))
    .mockResolvedValueOnce(new Response(JSON.stringify({ confirmTerminate: false })));

  const events: CustomEvent[] = [];
  const listener = (e: Event) => events.push(e as CustomEvent);
  window.addEventListener(PREFERENCES_EVENT, listener);

  render(<PreferencesPanel />);
  const box = (await screen.findByLabelText(/ask before terminating/i)) as HTMLInputElement;
  // The panel starts at false, so this only passes if the fetched value was applied.
  expect(box.checked).toBe(true);

  await userEvent.click(box);
  await userEvent.click(screen.getByText("Save"));

  await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(3));
  const put = fetchMock.mock.calls[1];
  expect(String(put[0])).toContain("/api/settings/preferences");
  expect((put[1] as RequestInit).method).toBe("PUT");
  expect(JSON.parse((put[1] as RequestInit).body as string)).toEqual({ confirmTerminate: false });
  expect(events).toHaveLength(1);
  expect(events[0].detail).toEqual({ confirmTerminate: false });

  window.removeEventListener(PREFERENCES_EVENT, listener);
});

test("preferences panel surfaces a load failure with a retry", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response("nope", { status: 500 }));

  render(<PreferencesPanel />);
  await screen.findByText("Retry");
  expect(screen.queryByLabelText(/ask before terminating/i)).not.toBeInTheDocument();
});

test("app applies the fetched preference and follows the update event", async () => {
  const sessions = [{ id: 1, tmuxName: "mm-1", toolId: 1, dir: "/a", status: "running" }];
  const layout = { shape: { rows: 1, cols: 2 }, tiles: [{ serverId: "local", sessionId: 1 }, null] };
  const routes: Record<string, () => Response> = {
    "/healthz": () => new Response(JSON.stringify({ status: "ok", setupPending: false, version: "test" })),
    "/api/auth/me": () => new Response("{}", { status: 200 }),
    // The startup fetch says "confirm before terminating".
    "/api/settings/preferences": () => new Response(JSON.stringify({ confirmTerminate: true })),
    "/api/layout": () => new Response(JSON.stringify(layout)),
    "/api/sessions": () => new Response(JSON.stringify(sessions)),
    "/api/tools": () => new Response(JSON.stringify([{ id: 1, name: "claude", command: "claude" }])),
    "/api/dirs": () => new Response(JSON.stringify([{ id: 1, name: "multimux", path: "/a" }])),
  };
  vi.spyOn(globalThis, "fetch").mockImplementation((input, init) => {
    const url = String(input);
    if (url.includes("/api/sessions/") && init?.method === "DELETE") return Promise.resolve(new Response("{}"));
    if (url.includes("/api/layout") && init?.method === "PUT") return Promise.resolve(new Response("{}"));
    for (const [path, make] of Object.entries(routes)) {
      if (url.includes(path)) return Promise.resolve(make());
    }
    return Promise.reject(new Error(`unmocked fetch: ${url}`));
  });
  // Declining keeps the session, so the terminate control stays available.
  const confirmMock = vi.spyOn(window, "confirm").mockReturnValue(false);

  render(<App />);
  await screen.findByTestId("term-1");

  // The fetched value must actually reach GridPage.
  await userEvent.click(screen.getByLabelText("terminate session 1"));
  expect(confirmMock).toHaveBeenCalledTimes(1);
  expect(screen.getByTestId("term-1")).toBeInTheDocument();

  // Saving the panel turns confirmation off; App must follow the event.
  act(() => {
    window.dispatchEvent(new CustomEvent(PREFERENCES_EVENT, { detail: { confirmTerminate: false } }));
  });

  await userEvent.click(screen.getByLabelText("terminate session 1"));
  await waitFor(() => expect(screen.queryByTestId("term-1")).not.toBeInTheDocument());
  expect(confirmMock).toHaveBeenCalledTimes(1);
});
