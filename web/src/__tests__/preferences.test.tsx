import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, vi } from "vitest";
import PreferencesPanel, { PREFERENCES_EVENT } from "../settings/PreferencesPanel";

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
