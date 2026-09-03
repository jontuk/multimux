import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { vi } from "vitest";
import PaneTextReader from "../grid/PaneTextReader";
import type { Server } from "../servers";

const local: Server = { id: "local", origin: "https://local.test", name: "local" };
const clipboardDescriptor = Object.getOwnPropertyDescriptor(navigator, "clipboard");

function Harness() {
  const [open, setOpen] = useState(false);
  const [trigger, setTrigger] = useState<HTMLButtonElement | null>(null);
  return (
    <>
      <button
        onClick={(event) => {
          setTrigger(event.currentTarget);
          setOpen(true);
        }}
      >
        Open pane text
      </button>
      {open && (
        <PaneTextReader
          server={local}
          sessionId={7}
          title="#7 · claude"
          open
          onClose={() => setOpen(false)}
          trigger={trigger}
        />
      )}
    </>
  );
}

function deferredResponse() {
  let resolve!: (response: Response) => void;
  const promise = new Promise<Response>((done) => (resolve = done));
  return { promise, resolve };
}

function paneTextResponse(text: string, processor: "codex" | "claude" | "raw" = "codex", warning = "") {
  const model = processor === "codex" ? "gpt-5.6-luna" : processor === "claude" ? "sonnet-5" : "";
  return new Response(JSON.stringify({ text, processor, model, warning }));
}

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  if (clipboardDescriptor) Object.defineProperty(navigator, "clipboard", clipboardDescriptor);
  else Object.defineProperty(navigator, "clipboard", { configurable: true, value: undefined });
});

test("opens immediately, cleans pane text with Codex, and posts to the clean endpoint", async () => {
  const pending = deferredResponse();
  const fetchMock = vi.spyOn(globalThis, "fetch").mockReturnValue(pending.promise);
  vi.spyOn(HTMLElement.prototype, "scrollHeight", "get").mockReturnValue(640);
  render(<Harness />);
  await userEvent.click(screen.getByRole("button", { name: "Open pane text" }));
  const dialog = screen.getByRole("dialog", { name: "Pane text for #7 · claude" });
  expect(dialog).toHaveAttribute("aria-modal", "true");
  expect(within(dialog).getByText("Capturing and cleaning pane text…")).toBeInTheDocument();
  expect(within(dialog).getByRole("button", { name: "Close" })).toHaveFocus();
  expect(fetchMock).toHaveBeenNthCalledWith(
    1,
    "https://local.test/api/sessions/7/text/clean",
    expect.objectContaining({ method: "POST" }),
  );
  await act(async () => pending.resolve(paneTextResponse("first joined paragraph\n")));
  const text = await within(dialog).findByTestId("pane-text-content");
  expect(text.textContent).toBe("first joined paragraph\n");
  expect(within(dialog).getByText("Cleaned with Codex (gpt-5.6-luna).")).toBeInTheDocument();
  expect(text.scrollTop).toBe(640);
});

test("an empty snapshot succeeds but disables Copy all", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(paneTextResponse("", "raw"));
  render(<Harness />);
  await userEvent.click(screen.getByRole("button", { name: "Open pane text" }));
  await screen.findByTestId("pane-text-content");
  expect(screen.getByRole("button", { name: "Copy all" })).toBeDisabled();
  expect(screen.getByText("No cleanup needed.")).toBeInTheDocument();
  expect(screen.queryByText("Capturing and cleaning pane text…")).not.toBeInTheDocument();
});

test("initial failure offers Retry and Close", async () => {
  const fetchMock = vi
    .spyOn(globalThis, "fetch")
    .mockResolvedValueOnce(new Response('{"error":"session is no longer available"}', { status: 409 }))
    .mockResolvedValueOnce(paneTextResponse("recovered"));
  render(<Harness />);
  await userEvent.click(screen.getByRole("button", { name: "Open pane text" }));
  expect(await screen.findByText(/session is no longer available/)).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Retry" }));
  expect(await screen.findByTestId("pane-text-content")).toHaveTextContent("recovered");
  expect(fetchMock).toHaveBeenCalledTimes(2);
});

test("refresh retains the old snapshot and leaves it after failure", async () => {
  const refresh = deferredResponse();
  vi.spyOn(globalThis, "fetch")
    .mockResolvedValueOnce(paneTextResponse("old snapshot"))
    .mockReturnValueOnce(refresh.promise);
  render(<Harness />);
  await userEvent.click(screen.getByRole("button", { name: "Open pane text" }));
  const content = await screen.findByTestId("pane-text-content");
  await userEvent.click(screen.getByRole("button", { name: "Refresh" }));
  expect(content).toHaveTextContent("old snapshot");
  expect(screen.getByText("Refreshing and cleaning…")).toBeInTheDocument();
  await act(async () => refresh.resolve(new Response('{"error":"session is no longer available"}', { status: 409 })));
  expect(content).toHaveTextContent("old snapshot");
  expect(await screen.findByText(/session is no longer available/)).toBeInTheDocument();
});

test("successful refresh replaces text and scrolls to the new bottom", async () => {
  vi.spyOn(globalThis, "fetch")
    .mockResolvedValueOnce(paneTextResponse("old"))
    .mockResolvedValueOnce(paneTextResponse("newest"));
  vi.spyOn(HTMLElement.prototype, "scrollHeight", "get").mockReturnValue(720);
  render(<Harness />);
  await userEvent.click(screen.getByRole("button", { name: "Open pane text" }));
  const content = await screen.findByTestId("pane-text-content");
  content.scrollTop = 0;
  await userEvent.click(screen.getByRole("button", { name: "Refresh" }));
  await waitFor(() => expect(content).toHaveTextContent("newest"));
  expect(content.scrollTop).toBe(720);
});

test("reports Claude cleanup and then an explicit raw fallback warning", async () => {
  const warning = "Automatic cleanup failed with Codex. Showing raw pane text.";
  vi.spyOn(globalThis, "fetch")
    .mockResolvedValueOnce(paneTextResponse("cleaned by Claude\n", "claude"))
    .mockResolvedValueOnce(paneTextResponse("raw\npane\ntext\n", "raw", warning));
  render(<Harness />);
  await userEvent.click(screen.getByRole("button", { name: "Open pane text" }));
  expect(await screen.findByText("Cleaned with Claude (sonnet-5).")).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Refresh" }));
  const content = await screen.findByTestId("pane-text-content");
  await waitFor(() => expect(content.textContent).toBe("raw\npane\ntext\n"));
  expect(screen.getByText(warning)).toHaveClass("error");
});

test("a slow older generation cannot overwrite a newer one", async () => {
  const first = deferredResponse();
  const second = deferredResponse();
  vi.spyOn(globalThis, "fetch").mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
  render(<Harness />);
  await userEvent.click(screen.getByRole("button", { name: "Open pane text" }));
  await userEvent.click(screen.getByRole("button", { name: "Refresh" }));
  await act(async () => second.resolve(paneTextResponse("new generation")));
  expect(await screen.findByTestId("pane-text-content")).toHaveTextContent("new generation");
  await act(async () => first.resolve(paneTextResponse("stale generation")));
  expect(screen.getByTestId("pane-text-content")).toHaveTextContent("new generation");
});

test("Copy all announces success and preserves text on rejection", async () => {
  const writeText = vi.fn().mockResolvedValueOnce(undefined).mockRejectedValueOnce(new Error("denied"));
  Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
  vi.spyOn(globalThis, "fetch").mockResolvedValue(paneTextResponse("copy me"));
  render(<Harness />);
  await userEvent.click(screen.getByRole("button", { name: "Open pane text" }));
  await screen.findByText("copy me");
  await userEvent.click(screen.getByRole("button", { name: "Copy all" }));
  expect(writeText).toHaveBeenCalledWith("copy me");
  expect(screen.getByText("Copied pane text.")).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Copy all" }));
  expect(screen.getByTestId("pane-text-content")).toHaveTextContent("copy me");
  expect(screen.getByText(/select the text and copy it manually/i)).toBeInTheDocument();
});

test("missing Clipboard API gives manual-copy guidance", async () => {
  Object.defineProperty(navigator, "clipboard", { configurable: true, value: undefined });
  vi.spyOn(globalThis, "fetch").mockResolvedValue(paneTextResponse("copy me"));
  render(<Harness />);
  await userEvent.click(screen.getByRole("button", { name: "Open pane text" }));
  await screen.findByText("copy me");
  await userEvent.click(screen.getByRole("button", { name: "Copy all" }));
  expect(screen.getByText(/select the text and copy it manually/i)).toBeInTheDocument();
});

test("close aborts, discards the snapshot, and restores trigger focus", async () => {
  let signal: AbortSignal | null | undefined;
  const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation((_input, init) => {
    signal = init?.signal;
    return Promise.resolve(paneTextResponse("secret snapshot"));
  });
  render(<Harness />);
  const trigger = screen.getByRole("button", { name: "Open pane text" });
  await userEvent.click(trigger);
  await screen.findByText("secret snapshot");
  await userEvent.click(screen.getByRole("button", { name: "Close" }));
  expect(signal?.aborted).toBe(true);
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(trigger).toHaveFocus();
  const reopened = deferredResponse();
  fetchMock.mockReturnValueOnce(reopened.promise);
  await userEvent.click(trigger);
  expect(screen.getByText("Capturing and cleaning pane text…")).toBeInTheDocument();
  await act(async () => reopened.resolve(paneTextResponse("fresh snapshot")));
  expect(await screen.findByText("fresh snapshot")).toBeInTheDocument();
});

test("a result that resolves after close stays inert", async () => {
  const pending = deferredResponse();
  vi.spyOn(globalThis, "fetch").mockReturnValue(pending.promise);
  render(<Harness />);
  const trigger = screen.getByRole("button", { name: "Open pane text" });
  await userEvent.click(trigger);
  await userEvent.click(screen.getByRole("button", { name: "Close" }));
  await act(async () => pending.resolve(paneTextResponse("late secret")));
  expect(screen.queryByText("late secret")).not.toBeInTheDocument();
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});

test("Escape closes and Tab wraps inside the modal", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(paneTextResponse("text"));
  render(<Harness />);
  const trigger = screen.getByRole("button", { name: "Open pane text" });
  await userEvent.click(trigger);
  await screen.findByText("text");
  const dialog = screen.getByRole("dialog");
  const buttons = within(dialog).getAllByRole("button");
  const content = screen.getByTestId("pane-text-content");
  content.focus();
  fireEvent.keyDown(dialog, { key: "Tab" });
  expect(buttons[0]).toHaveFocus();
  buttons[0].focus();
  fireEvent.keyDown(dialog, { key: "Tab", shiftKey: true });
  expect(content).toHaveFocus();
  fireEvent.keyDown(dialog, { key: "Escape" });
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(trigger).toHaveFocus();
});
