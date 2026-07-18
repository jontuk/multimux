import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";
import App from "../App";
import AppearancePanel, { APPEARANCE_EVENT } from "../settings/AppearancePanel";

function mockFetchByURL(routes: Record<string, () => Response>) {
  return vi.spyOn(globalThis, "fetch").mockImplementation((input) => {
    const url = String(input);
    for (const [path, make] of Object.entries(routes)) {
      if (url.includes(path)) return Promise.resolve(make());
    }
    return Promise.reject(new Error(`unmocked fetch: ${url}`));
  });
}

test("header shows host label and accent from healthz", async () => {
  const fetchMock = mockFetchByURL({
    "/healthz": () =>
      new Response(
        JSON.stringify({
          status: "ok",
          setupPending: false,
          version: "test",
          hostLabel: "work-mac",
          accentColor: "#3fb950",
        }),
      ),
    "/api/auth/me": () => new Response("{}", { status: 200 }),
    "/api/sessions": () => new Response("[]"),
    "/api/layout": () => new Response("{}"),
    "/api/tools": () => new Response("[]"),
    "/api/dirs": () => new Response("[]"),
  });

  const { container } = render(<App />);
  await screen.findByText("@work-mac");
  const header = container.querySelector("header.host-accented") as HTMLElement;
  expect(header).not.toBeNull();
  expect(header.style.getPropertyValue("--host-accent")).toBe("#3fb950");
  fetchMock.mockRestore();
});

test("appearance panel loads and saves, dispatching update event", async () => {
  const fetchMock = vi
    .spyOn(globalThis, "fetch")
    .mockResolvedValueOnce(
      new Response(JSON.stringify({ hostLabel: "", accentColor: "", osHostname: "jons-mac.local" })),
    )
    .mockResolvedValueOnce(new Response(JSON.stringify({ ok: true })))
    .mockResolvedValueOnce(
      new Response(JSON.stringify({ hostLabel: "work-mac", accentColor: "", osHostname: "jons-mac.local" })),
    );

  const events: CustomEvent[] = [];
  const listener = (e: Event) => events.push(e as CustomEvent);
  window.addEventListener(APPEARANCE_EVENT, listener);

  render(<AppearancePanel />);
  const labelInput = await screen.findByPlaceholderText("jons-mac.local");
  await userEvent.type(labelInput, "work-mac");
  await userEvent.click(screen.getByText("Save"));

  await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(3));
  const put = fetchMock.mock.calls[1];
  expect(String(put[0])).toContain("/api/settings/appearance");
  expect((put[1] as RequestInit).method).toBe("PUT");
  expect(JSON.parse((put[1] as RequestInit).body as string)).toEqual({ hostLabel: "work-mac", accentColor: "" });
  expect(events).toHaveLength(1);
  expect(events[0].detail).toEqual({ hostLabel: "work-mac", accentColor: "" });

  window.removeEventListener(APPEARANCE_EVENT, listener);
  fetchMock.mockRestore();
});
