import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
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

function PersistentHarness() {
  const [open, setOpen] = useState(true);
  return (
    <>
      <button onClick={() => setOpen(true)}>Open reader</button>
      <button onClick={() => setOpen(false)}>Hide reader</button>
      <PaneTextReader
        server={local}
        sessionId={7}
        title="#7 · claude"
        open={open}
        onClose={() => setOpen(false)}
        trigger={null}
      />
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

test("pane text feedback errors use the error color", () => {
  const styles = readFileSync(resolve(process.cwd(), "src/index.css"), "utf8");
  const errorRule = styles.match(/\.pane-text-feedback \.error\s*\{([^}]*)\}/s)?.[1];

  expect(errorRule).toMatch(/color:\s*var\(--error\)/);
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
  const retry = deferredResponse();
  vi.spyOn(globalThis, "fetch")
    .mockResolvedValueOnce(paneTextResponse("old snapshot"))
    .mockReturnValueOnce(refresh.promise)
    .mockReturnValueOnce(retry.promise);
  render(<Harness />);
  await userEvent.click(screen.getByRole("button", { name: "Open pane text" }));
  const content = await screen.findByTestId("pane-text-content");
  expect(screen.getByText("Cleaned with Codex (gpt-5.6-luna).")).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Refresh" }));
  expect(content).toHaveTextContent("old snapshot");
  expect(screen.getByText("Refreshing and cleaning…")).toBeInTheDocument();
  expect(screen.getByText("Cleaned with Codex (gpt-5.6-luna).")).toBeInTheDocument();
  await act(async () => refresh.resolve(new Response('{"error":"session is no longer available"}', { status: 409 })));
  expect(content).toHaveTextContent("old snapshot");
  const refreshError = await screen.findByText(/session is no longer available/);
  expect(refreshError).toHaveClass("error");
  expect(screen.getByText("Cleaned with Codex (gpt-5.6-luna).")).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Refresh" }));
  expect(screen.getByText("Refreshing and cleaning…")).toBeInTheDocument();
  expect(screen.getByText("Cleaned with Codex (gpt-5.6-luna).")).toBeInTheDocument();
});

test("a raw fallback warning remains alongside a later refresh error", async () => {
  const warning = "Automatic cleanup failed with Codex. Showing raw pane text.";
  vi.spyOn(globalThis, "fetch")
    .mockResolvedValueOnce(paneTextResponse("retained raw snapshot", "raw", warning))
    .mockResolvedValueOnce(new Response('{"error":"new capture failed"}', { status: 500 }));
  render(<Harness />);
  await userEvent.click(screen.getByRole("button", { name: "Open pane text" }));
  const content = await screen.findByTestId("pane-text-content");
  expect(screen.getByText(warning)).toHaveClass("error");

  await userEvent.click(screen.getByRole("button", { name: "Refresh" }));

  expect(content).toHaveTextContent("retained raw snapshot");
  expect(screen.getByText(warning)).toHaveClass("error");
  const refreshError = await screen.findByText(/new capture failed/);
  expect(refreshError).toHaveClass("error");
  expect(refreshError.closest(".pane-text-feedback")).toContainElement(screen.getByText(warning));
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
  const writeText = vi.fn().mockResolvedValueOnce(undefined).mockRejectedValueOnce(new Error("denied"));
  Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
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
  await userEvent.click(screen.getByRole("button", { name: "Copy all" }));
  expect(screen.getByText("Copied pane text.")).toBeInTheDocument();
  expect(screen.getByText(warning)).toHaveClass("error");
  await userEvent.click(screen.getByRole("button", { name: "Copy all" }));
  expect(screen.getByText(/select the text and copy it manually/i)).toBeInTheDocument();
  expect(screen.getByText(warning)).toHaveClass("error");
});

test("a slow older generation cannot overwrite newer text or cleanup metadata", async () => {
  const first = deferredResponse();
  const second = deferredResponse();
  vi.spyOn(globalThis, "fetch").mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
  render(<Harness />);
  await userEvent.click(screen.getByRole("button", { name: "Open pane text" }));
  await userEvent.click(screen.getByRole("button", { name: "Refresh" }));
  await act(async () => second.resolve(paneTextResponse("new generation", "claude")));
  expect(await screen.findByTestId("pane-text-content")).toHaveTextContent("new generation");
  expect(screen.getByText("Cleaned with Claude (sonnet-5).")).toBeInTheDocument();
  const staleWarning = "Automatic cleanup failed with Codex. Showing stale raw pane text.";
  await act(async () => first.resolve(paneTextResponse("stale generation", "raw", staleWarning)));
  expect(screen.getByTestId("pane-text-content")).toHaveTextContent("new generation");
  expect(screen.getByText("Cleaned with Claude (sonnet-5).")).toBeInTheDocument();
  expect(screen.queryByText(staleWarning)).not.toBeInTheDocument();
});

test("a clipboard completion from an older generation cannot mask a refresh warning", async () => {
  let resolveCopy!: () => void;
  const pendingCopy = new Promise<void>((resolve) => (resolveCopy = resolve));
  const writeText = vi.fn().mockReturnValue(pendingCopy);
  Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
  const warning = "Automatic cleanup failed with Codex. Showing raw pane text.";
  vi.spyOn(globalThis, "fetch")
    .mockResolvedValueOnce(paneTextResponse("old cleaned text"))
    .mockResolvedValueOnce(paneTextResponse("new raw text", "raw", warning));
  render(<Harness />);
  await userEvent.click(screen.getByRole("button", { name: "Open pane text" }));
  await screen.findByText("old cleaned text");
  await userEvent.click(screen.getByRole("button", { name: "Copy all" }));
  await userEvent.click(screen.getByRole("button", { name: "Refresh" }));
  expect(await screen.findByText("new raw text")).toBeInTheDocument();
  expect(screen.getByText(warning)).toHaveClass("error");
  await act(async () => resolveCopy());
  expect(screen.getByText(warning)).toHaveClass("error");
  expect(screen.queryByText("Copied pane text.")).not.toBeInTheDocument();
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
  expect(screen.getByText("Cleaned with Codex (gpt-5.6-luna).")).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Copy all" }));
  expect(screen.getByTestId("pane-text-content")).toHaveTextContent("copy me");
  expect(screen.getByText(/select the text and copy it manually/i)).toBeInTheDocument();
  expect(screen.getByText("Cleaned with Codex (gpt-5.6-luna).")).toBeInTheDocument();
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
  let signal: AbortSignal | null | undefined;
  vi.spyOn(globalThis, "fetch").mockImplementation((_input, init) => {
    signal = init?.signal;
    return pending.promise;
  });
  render(<Harness />);
  const trigger = screen.getByRole("button", { name: "Open pane text" });
  await userEvent.click(trigger);
  await userEvent.click(screen.getByRole("button", { name: "Close" }));
  expect(signal?.aborted).toBe(true);
  await act(async () => pending.resolve(paneTextResponse("late secret")));
  expect(screen.queryByText("late secret")).not.toBeInTheDocument();
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});

test("an externally hidden reader clears its snapshot before reopening", async () => {
  const reopened = deferredResponse();
  vi.spyOn(globalThis, "fetch")
    .mockResolvedValueOnce(paneTextResponse("previous snapshot"))
    .mockReturnValueOnce(reopened.promise);
  render(<PersistentHarness />);
  expect(await screen.findByText("previous snapshot")).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Hide reader" }));
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Open reader" }));
  expect(screen.getByText("Capturing and cleaning pane text…")).toBeInTheDocument();
  expect(screen.queryByText("previous snapshot")).not.toBeInTheDocument();
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
