import { fireEvent, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { forwardRef, Profiler, useEffect, useImperativeHandle } from "react";
import { createPortal } from "react-dom";
import { vi } from "vitest";
import MobileSessionView from "../grid/MobileSessionView";
import type { MobileSession } from "../grid/mobileModel";
import type { Session, Tool } from "../grid/types";
import type { Server } from "../servers";
import type { TerminalHandle } from "../term/TerminalTile";

const unmounted = vi.fn();
const terminalAttached = vi.fn();
const terminalHandles = new Map<number, TerminalHandle>();
const terminalCalls: Array<{ sessionId: number; operation: "input" | "paste"; data: string }> = [];
const terminalFontSizes: Array<{ sessionId: number; size: number }> = [];
let terminalConnected = true;
let terminalInputAccepted = true;

vi.mock("../term/TerminalTile", () => ({
  default: forwardRef(function TerminalTileMock(
    {
      sessionId,
      sizePolicy,
      controlsSlot,
      touchScrollback,
    }: {
      sessionId: number;
      sizePolicy?: string;
      controlsSlot?: HTMLElement | null;
      touchScrollback?: boolean;
    },
    ref,
  ) {
    let handle = terminalHandles.get(sessionId);
    if (!handle) {
      handle = {
        input(data) {
          if (!terminalConnected || !terminalInputAccepted) return false;
          terminalCalls.push({ sessionId, operation: "input", data });
          return true;
        },
        paste(data) {
          if (!terminalConnected) return false;
          terminalCalls.push({ sessionId, operation: "paste", data });
          return true;
        },
        focus() {},
        setFontSize(size) {
          terminalFontSizes.push({ sessionId, size });
        },
        fit() {},
      };
      terminalHandles.set(sessionId, handle);
    }
    useImperativeHandle(ref, () => handle, [handle]);
    useEffect(() => {
      terminalAttached(sessionId);
      return () => unmounted(sessionId);
    }, [sessionId]);
    const fit = <button aria-label="Fit session to phone">Fit</button>;
    return (
      <>
        <div
          data-testid={`term-${sessionId}`}
          data-size-policy={sizePolicy}
          data-controls-slot={controlsSlot?.className}
          data-touch-scrollback={touchScrollback ? "true" : "false"}
        />
        {controlsSlot ? createPortal(fit, controlsSlot) : fit}
      </>
    );
  }),
}));

vi.mock("../grid/MobileSessionCreator", () => ({
  default: ({
    onCancel,
    onLaunched,
  }: {
    onCancel: () => void;
    onLaunched: (server: Server, sessions: Session[]) => void;
  }) => (
    <section aria-label="New session">
      <button type="button" aria-label="Close new session creator" onClick={onCancel}>
        Back
      </button>
      <button type="button" onClick={() => onLaunched(local, [session(31).session, session(32).session])}>
        Complete grouped launch
      </button>
    </section>
  ),
}));

const local: Server = {
  id: "local",
  origin: "https://local.test",
  name: "local",
};

const tools: Tool[] = [
  { id: 1, name: "claude", command: "claude" },
  { id: 2, name: "zsh", command: "zsh" },
];

function session(id: number, overrides: Partial<Session> = {}): MobileSession {
  return {
    key: `local:${id}`,
    server: local,
    session: {
      id,
      tmuxName: `mm-${id}`,
      toolId: 1,
      dir: `/work/${id}`,
      status: "running",
      ...overrides,
    },
  };
}

function swipe(
  header: HTMLElement,
  {
    fromX = 100,
    fromY = 10,
    toX,
    toY = 10,
    pointerId = 1,
    isPrimary = true,
  }: {
    fromX?: number;
    fromY?: number;
    toX: number;
    toY?: number;
    pointerId?: number;
    isPrimary?: boolean;
  },
) {
  fireEvent.pointerDown(header, { pointerId, isPrimary, clientX: fromX, clientY: fromY });
  fireEvent.pointerUp(header, { pointerId, isPrimary, clientX: toX, clientY: toY });
}

function mockPointerCapture(header: HTMLElement) {
  const captured = new Set<number>();
  const setPointerCapture = vi.fn((pointerId: number) => captured.add(pointerId));
  const releasePointerCapture = vi.fn((pointerId: number) => captured.delete(pointerId));
  Object.assign(header, {
    hasPointerCapture: (pointerId: number) => captured.has(pointerId),
    releasePointerCapture,
    setPointerCapture,
  });
  return { captured, releasePointerCapture, setPointerCapture };
}

beforeEach(() => {
  unmounted.mockClear();
  terminalAttached.mockClear();
  terminalHandles.clear();
  terminalCalls.length = 0;
  terminalFontSizes.length = 0;
  terminalConnected = true;
  terminalInputAccepted = true;
  localStorage.clear();
});

afterEach(() => vi.restoreAllMocks());

test("orders mobile actions as New, Text, Fit, Compose, font, Settings", () => {
  render(
    <MobileSessionView
      servers={[local]}
      sessions={[session(1)]}
      toolsByServer={{ local: tools }}
      initialLoading={false}
      onRefresh={vi.fn()}
    />,
  );

  const actions = document.querySelector<HTMLElement>(".mobile-session-actions")!;
  expect(
    Array.from(actions.querySelectorAll("button, select, a")).map(
      (node) => node.getAttribute("aria-label") ?? node.textContent,
    ),
  ).toEqual([
    "New session",
    "Read text from session 1",
    "Fit session to phone",
    "Compose",
    "Terminal font size",
    "Settings",
  ]);
});

test("mobile Text targets the selection without reconnecting its terminal", async () => {
  const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response("mobile snapshot"));
  render(
    <MobileSessionView
      servers={[local]}
      sessions={[session(1)]}
      toolsByServer={{ local: tools }}
      initialLoading={false}
      onRefresh={vi.fn()}
    />,
  );
  const textButton = screen.getByRole("button", { name: "Read text from session 1" });
  await userEvent.click(textButton);
  expect(await screen.findByRole("dialog", { name: "Pane text for #1 · claude" })).toBeInTheDocument();
  expect(screen.getByText("mobile snapshot")).toBeInTheDocument();
  expect(fetchMock.mock.calls[0][0]).toBe("https://local.test/api/sessions/1/text");
  expect(screen.getByTestId("term-1")).toBeInTheDocument();
  expect(terminalHandles.size).toBe(1);
  expect(terminalAttached).toHaveBeenCalledTimes(1);
  expect(unmounted).not.toHaveBeenCalled();
  await userEvent.click(screen.getByRole("button", { name: "Refresh" }));
  await userEvent.click(screen.getByRole("button", { name: "Close" }));
  expect(terminalHandles.size).toBe(1);
  expect(terminalAttached).toHaveBeenCalledTimes(1);
  expect(unmounted).not.toHaveBeenCalled();
  expect(textButton).toHaveFocus();
});

test("mobile Text is absent while loading and when no session is selected", () => {
  const props = { servers: [local], sessions: [], toolsByServer: {}, onRefresh: vi.fn() };
  const { rerender } = render(<MobileSessionView {...props} initialLoading />);
  expect(screen.queryByRole("button", { name: /Read text from session/ })).not.toBeInTheDocument();
  rerender(<MobileSessionView {...props} initialLoading={false} />);
  expect(screen.queryByRole("button", { name: /Read text from session/ })).not.toBeInTheDocument();
});

test("New session remains available while loading and when empty", () => {
  const props = { servers: [local], sessions: [], toolsByServer: {}, onRefresh: vi.fn() };
  const { rerender } = render(<MobileSessionView {...props} initialLoading />);
  expect(screen.getByRole("button", { name: "New session" })).toBeInTheDocument();
  rerender(<MobileSessionView {...props} initialLoading={false} />);
  expect(screen.getByRole("button", { name: "New session" })).toBeInTheDocument();
  expect(screen.queryByText(/launching needs a wider device/i)).not.toBeInTheDocument();
});

test("opening and cancelling the creator preserves the mounted terminal and Compose draft", async () => {
  render(
    <MobileSessionView
      servers={[local]}
      sessions={[session(1)]}
      toolsByServer={{ local: tools }}
      initialLoading={false}
      onRefresh={vi.fn()}
    />,
  );
  await userEvent.click(screen.getByRole("button", { name: "Compose" }));
  fireEvent.change(screen.getByRole("textbox", { name: "Compose terminal input" }), {
    target: { value: "keep me" },
  });

  await userEvent.click(screen.getByRole("button", { name: "New session" }));
  expect(document.querySelector(".mobile-session-browser")).toHaveAttribute("hidden");
  expect(unmounted).not.toHaveBeenCalled();
  await userEvent.click(screen.getByRole("button", { name: "Close new session creator" }));

  expect(screen.getByTestId("term-1")).toBeInTheDocument();
  expect(screen.getByRole("textbox", { name: "Compose terminal input" })).toHaveValue("keep me");
  expect(screen.getByRole("button", { name: "New session" })).toHaveFocus();
});

test("selects the first grouped result only after it appears in refreshed sessions", async () => {
  const onRefresh = vi.fn();
  const { rerender } = render(
    <MobileSessionView
      servers={[local]}
      sessions={[session(1)]}
      toolsByServer={{ local: tools }}
      initialLoading={false}
      onRefresh={onRefresh}
    />,
  );
  await userEvent.click(screen.getByRole("button", { name: "New session" }));
  await userEvent.click(screen.getByRole("button", { name: "Complete grouped launch" }));
  expect(onRefresh).toHaveBeenCalledTimes(1);
  expect(screen.getByTestId("term-1")).toBeInTheDocument();

  rerender(
    <MobileSessionView
      servers={[local]}
      sessions={[session(1), session(31), session(32)]}
      toolsByServer={{ local: tools }}
      initialLoading={false}
      onRefresh={onRefresh}
    />,
  );
  expect(screen.getByTestId("term-31")).toBeInTheDocument();
  expect(screen.getByText("2/3")).toBeInTheDocument();
});

test("mounts the selected mobile terminal with passive sizing", () => {
  render(
    <MobileSessionView
      servers={[local]}
      sessions={[session(1)]}
      toolsByServer={{ local: tools }}
      initialLoading={false}
      onRefresh={vi.fn()}
    />,
  );

  expect(screen.getByTestId("term-1")).toHaveAttribute("data-size-policy", "passive");
  expect(screen.getByTestId("term-1")).toHaveAttribute("data-touch-scrollback", "true");
});

test("selects and persists a mobile terminal font size", async () => {
  render(
    <MobileSessionView
      servers={[local]}
      sessions={[session(1)]}
      toolsByServer={{ local: tools }}
      initialLoading={false}
      onRefresh={vi.fn()}
    />,
  );

  const select = screen.getByRole("combobox", { name: "Terminal font size" });
  expect(
    within(select)
      .getAllByRole("option")
      .map((option) => option.textContent),
  ).toEqual(["13 px", "11 px", "10 px", "9 px"]);
  expect(select).toHaveValue("13");

  await userEvent.selectOptions(select, "11");

  expect(select).toHaveValue("11");
  expect(terminalFontSizes.at(-1)).toEqual({ sessionId: 1, size: 11 });
  expect(localStorage.getItem("multimux.mobileFontSize")).toBe("11");
});

test("restores the mobile font size and reapplies it after session switching", () => {
  localStorage.setItem("multimux.mobileFontSize", "9");
  render(
    <MobileSessionView
      servers={[local]}
      sessions={[session(1), session(2)]}
      toolsByServer={{ local: tools }}
      initialLoading={false}
      onRefresh={vi.fn()}
    />,
  );

  expect(screen.getByRole("combobox", { name: "Terminal font size" })).toHaveValue("9");
  expect(terminalFontSizes.at(-1)).toEqual({ sessionId: 1, size: 9 });

  swipe(document.querySelector<HTMLElement>(".mobile-session-header")!, { toX: 52 });

  expect(terminalFontSizes.at(-1)).toEqual({ sessionId: 2, size: 9 });
});

test("distinguishes unresolved initial data from a settled empty session list", () => {
  const { rerender } = render(
    <MobileSessionView servers={[local]} sessions={[]} toolsByServer={{}} initialLoading onRefresh={vi.fn()} />,
  );

  expect(screen.getByText("Loading sessions…")).toBeInTheDocument();
  expect(screen.queryByText(/no sessions are running/i)).not.toBeInTheDocument();

  rerender(
    <MobileSessionView servers={[local]} sessions={[]} toolsByServer={{}} initialLoading={false} onRefresh={vi.fn()} />,
  );

  expect(screen.getByText(/no sessions are running/i)).toBeInTheDocument();
  expect(screen.queryByText(/launching needs a wider device/i)).not.toBeInTheDocument();
  expect(screen.queryByText("Loading sessions…")).not.toBeInTheDocument();
});

test("combines host, session context, controls, position, and Settings in one mobile header", () => {
  render(
    <MobileSessionView
      servers={[local]}
      sessions={[session(1, { branch: "mobile", gitState: "clean" })]}
      toolsByServer={{ local: tools }}
      initialLoading={false}
      onRefresh={vi.fn()}
      hostLabel="work-mac"
    />,
  );

  const header = document.querySelector<HTMLElement>(".mobile-session-header")!;
  expect(header).toHaveTextContent("@work-mac");
  expect(header).toHaveTextContent("#1 · claude");
  expect(header).toHaveTextContent("mobile");
  expect(header).toHaveTextContent("/work/1");
  expect(header).toHaveTextContent("1/1");
  expect(within(header).getByRole("link", { name: "Settings" })).toHaveAttribute("href", "#/settings");
  expect(header.querySelector(".mobile-terminal-controls")).not.toBeNull();
  expect(screen.getByTestId("term-1")).toHaveAttribute("data-controls-slot", "mobile-terminal-controls");
});

test("keeps interactive controls outside the slider and does not capture their pointers", () => {
  render(
    <MobileSessionView
      servers={[local]}
      sessions={[session(1)]}
      toolsByServer={{ local: tools }}
      initialLoading={false}
      onRefresh={vi.fn()}
    />,
  );

  const header = document.querySelector<HTMLElement>(".mobile-session-header")!;
  const slider = within(header).getByRole("slider", { name: "Active session" });
  const settings = within(header).getByRole("link", { name: "Settings" });
  const controls = header.querySelector<HTMLElement>(".mobile-terminal-controls")!;
  const { setPointerCapture } = mockPointerCapture(header);

  expect(header).not.toHaveAttribute("role");
  expect(slider).not.toContainElement(settings);
  expect(slider).not.toContainElement(controls);

  fireEvent.pointerDown(settings, { pointerId: 7, isPrimary: true, clientX: 10, clientY: 10 });
  expect(setPointerCapture).not.toHaveBeenCalled();
});

test("applies the configured host accent to the consolidated mobile header", () => {
  render(
    <MobileSessionView
      servers={[local]}
      sessions={[session(1)]}
      toolsByServer={{ local: tools }}
      initialLoading={false}
      onRefresh={vi.fn()}
      accentColor="#3fb950"
    />,
  );

  const header = document.querySelector<HTMLElement>(".mobile-session-header")!;
  expect(header).toHaveClass("host-accented");
  expect(header.style.getPropertyValue("--host-accent")).toBe("#3fb950");
});

test("keeps identity and Settings chrome while loading and after an empty result", () => {
  const { rerender } = render(
    <MobileSessionView
      servers={[local]}
      sessions={[]}
      toolsByServer={{}}
      initialLoading
      onRefresh={vi.fn()}
      hostLabel="work-mac"
    />,
  );

  const header = document.querySelector<HTMLElement>(".mobile-session-header")!;
  expect(header).toHaveTextContent("@work-mac");
  expect(within(header).getByRole("link", { name: "Settings" })).toBeInTheDocument();
  expect(header).not.toHaveAttribute("role");
  expect(screen.getByText("Loading sessions…")).toBeInTheDocument();

  rerender(
    <MobileSessionView
      servers={[local]}
      sessions={[]}
      toolsByServer={{}}
      initialLoading={false}
      onRefresh={vi.fn()}
      hostLabel="work-mac"
    />,
  );
  expect(document.querySelectorAll(".mobile-session-header")).toHaveLength(1);
  expect(screen.getByText(/no sessions are running/i)).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "Settings" })).toBeInTheDocument();
});

test("mounts one terminal and renders label, position, branch, tracking, and directory metadata", () => {
  const sessions = [
    session(1),
    session(2, {
      label: "api refactor",
      branch: "mobile",
      gitState: "modified",
      ahead: 2,
      behind: 1,
      dir: "/repo/mobile",
    }),
    session(3),
    session(4),
    session(5),
  ];
  render(
    <MobileSessionView
      servers={[local]}
      sessions={sessions}
      toolsByServer={{ local: tools }}
      initialLoading={false}
      onRefresh={vi.fn()}
    />,
  );

  const header = document.querySelector<HTMLElement>(".mobile-session-header")!;
  swipe(header, { toX: 52 });

  expect(screen.getAllByTestId(/^term-/)).toHaveLength(1);
  expect(screen.getByTestId("term-2")).toBeInTheDocument();
  expect(screen.getByText("#2 · api refactor")).toBeInTheDocument();
  expect(screen.getByText("2/5")).toBeInTheDocument();
  expect(screen.getByText("mobile")).toBeInTheDocument();
  expect(screen.getByText("↑2")).toBeInTheDocument();
  expect(screen.getByText("↓1")).toBeInTheDocument();
  expect(screen.getByText("/repo/mobile")).toBeInTheDocument();
  expect(document.querySelector(".git-dot-modified")).toHaveAttribute("title", "tracked files modified");
});

test("uses tool and tmux names when a session has no label", () => {
  const { rerender } = render(
    <MobileSessionView
      servers={[local]}
      sessions={[session(1)]}
      toolsByServer={{ local: tools }}
      initialLoading={false}
      onRefresh={vi.fn()}
    />,
  );
  expect(screen.getByText("#1 · claude")).toBeInTheDocument();

  rerender(
    <MobileSessionView
      servers={[local]}
      sessions={[session(1, { toolId: 99 })]}
      toolsByServer={{ local: tools }}
      initialLoading={false}
      onRefresh={vi.fn()}
    />,
  );
  expect(screen.getByText("#1 · mm-1")).toBeInTheDocument();
});

test("moves next on a left swipe, previous on a right swipe, and clamps at each end", () => {
  render(
    <MobileSessionView
      servers={[local]}
      sessions={[session(1), session(2)]}
      toolsByServer={{ local: tools }}
      initialLoading={false}
      onRefresh={vi.fn()}
    />,
  );
  const header = document.querySelector<HTMLElement>(".mobile-session-header")!;

  swipe(header, { toX: 52 });
  expect(screen.getByTestId("term-2")).toBeInTheDocument();
  swipe(header, { toX: 52 });
  expect(screen.getByTestId("term-2")).toBeInTheDocument();

  swipe(header, { fromX: 52, toX: 100 });
  expect(screen.getByTestId("term-1")).toBeInTheDocument();
  swipe(header, { fromX: 52, toX: 100 });
  expect(screen.getByTestId("term-1")).toBeInTheDocument();
});

test("exposes the selected session as a named keyboard-operable slider", () => {
  render(
    <MobileSessionView
      servers={[local]}
      sessions={[session(1), session(2)]}
      toolsByServer={{ local: tools }}
      initialLoading={false}
      onRefresh={vi.fn()}
    />,
  );

  const slider = screen.getByRole("slider", { name: "Active session" });
  expect(slider).toHaveAttribute("tabindex", "0");
  expect(slider).toHaveAttribute("aria-valuemin", "1");
  expect(slider).toHaveAttribute("aria-valuemax", "2");
  expect(slider).toHaveAttribute("aria-valuenow", "1");
  expect(slider).toHaveAttribute("aria-valuetext", "Session 1 of 2: #1 · claude");

  fireEvent.keyDown(slider, { key: "ArrowRight" });
  expect(screen.getByTestId("term-2")).toBeInTheDocument();
  expect(slider).toHaveAttribute("aria-valuenow", "2");
  expect(slider).toHaveAttribute("aria-valuetext", "Session 2 of 2: #2 · claude");

  fireEvent.keyDown(slider, { key: "ArrowRight" });
  expect(screen.getByTestId("term-2")).toBeInTheDocument();
  fireEvent.keyDown(slider, { key: "ArrowLeft" });
  fireEvent.keyDown(slider, { key: "ArrowLeft" });
  expect(screen.getByTestId("term-1")).toBeInTheDocument();
  expect(slider).toHaveAttribute("aria-valuenow", "1");
});

test("supports the complete slider keyboard navigation pattern", () => {
  render(
    <MobileSessionView
      servers={[local]}
      sessions={[session(1), session(2), session(3)]}
      toolsByServer={{ local: tools }}
      initialLoading={false}
      onRefresh={vi.fn()}
    />,
  );
  const slider = screen.getByRole("slider", { name: "Active session" });

  fireEvent.keyDown(slider, { key: "ArrowUp" });
  expect(screen.getByTestId("term-2")).toBeInTheDocument();
  fireEvent.keyDown(slider, { key: "End" });
  fireEvent.keyDown(slider, { key: "ArrowUp" });
  expect(screen.getByTestId("term-3")).toBeInTheDocument();
  expect(slider).toHaveAttribute("aria-valuenow", "3");

  fireEvent.keyDown(slider, { key: "ArrowDown" });
  expect(screen.getByTestId("term-2")).toBeInTheDocument();
  fireEvent.keyDown(slider, { key: "Home" });
  fireEvent.keyDown(slider, { key: "ArrowDown" });
  expect(screen.getByTestId("term-1")).toBeInTheDocument();
  expect(slider).toHaveAttribute("aria-valuenow", "1");
});

test("keeps the announced ordinal synchronized during a session reorder", () => {
  const commits: { sessionId: string | undefined; valueNow: string | null }[] = [];
  const containerRef: { current?: HTMLElement } = {};
  const onRender = () => {
    if (!containerRef.current) return;
    commits.push({
      sessionId: containerRef.current.querySelector<HTMLElement>("[data-testid^='term-']")?.dataset.testid,
      valueNow:
        containerRef.current.querySelector<HTMLElement>("[role='slider']")?.getAttribute("aria-valuenow") ?? null,
    });
  };
  const { container: rendered, rerender } = render(
    <Profiler id="mobile-session" onRender={onRender}>
      <MobileSessionView
        servers={[local]}
        sessions={[session(1), session(2)]}
        toolsByServer={{ local: tools }}
        initialLoading={false}
        onRefresh={vi.fn()}
      />
    </Profiler>,
  );
  containerRef.current = rendered;
  fireEvent.keyDown(screen.getByRole("slider"), { key: "ArrowRight" });
  commits.length = 0;

  rerender(
    <Profiler id="mobile-session" onRender={onRender}>
      <MobileSessionView
        servers={[local]}
        sessions={[session(2), session(1)]}
        toolsByServer={{ local: tools }}
        initialLoading={false}
        onRefresh={vi.fn()}
      />
    </Profiler>,
  );

  expect(commits).not.toContainEqual({ sessionId: "term-2", valueNow: "2" });
  expect(screen.getByRole("slider")).toHaveAttribute("aria-valuenow", "1");
  expect(screen.getByText("1/2")).toBeInTheDocument();
});

test("captures a primary swipe pointer and releases it when the gesture ends", () => {
  render(
    <MobileSessionView
      servers={[local]}
      sessions={[session(1), session(2)]}
      toolsByServer={{ local: tools }}
      initialLoading={false}
      onRefresh={vi.fn()}
    />,
  );
  const header = document.querySelector<HTMLElement>(".mobile-session-header")!;
  const capture = mockPointerCapture(header);

  fireEvent.pointerDown(header, { pointerId: 7, isPrimary: true, clientX: 100, clientY: 10 });
  expect(capture.setPointerCapture).toHaveBeenCalledWith(7);
  fireEvent.pointerUp(header, { pointerId: 7, isPrimary: true, clientX: 52, clientY: 10 });

  expect(capture.releasePointerCapture).toHaveBeenCalledWith(7);
  expect(capture.captured).not.toContain(7);
  expect(screen.getByTestId("term-2")).toBeInTheDocument();
});

test("does not replace an active capture with another primary pointer type", () => {
  render(
    <MobileSessionView
      servers={[local]}
      sessions={[session(1), session(2)]}
      toolsByServer={{ local: tools }}
      initialLoading={false}
      onRefresh={vi.fn()}
    />,
  );
  const header = document.querySelector<HTMLElement>(".mobile-session-header")!;
  const capture = mockPointerCapture(header);

  fireEvent.pointerDown(header, { pointerId: 7, isPrimary: true, clientX: 100, clientY: 10 });
  fireEvent.pointerDown(header, { pointerId: 8, isPrimary: true, clientX: 40, clientY: 10 });

  expect(capture.setPointerCapture).toHaveBeenCalledTimes(1);
  expect(capture.setPointerCapture).toHaveBeenCalledWith(7);

  fireEvent.pointerUp(header, { pointerId: 7, isPrimary: true, clientX: 52, clientY: 10 });
  expect(capture.releasePointerCapture).toHaveBeenCalledWith(7);
  expect(screen.getByTestId("term-2")).toBeInTheDocument();
});

test("ignores pointer-up and cancellation from a different pointer while a capture is active", () => {
  render(
    <MobileSessionView
      servers={[local]}
      sessions={[session(1), session(2)]}
      toolsByServer={{ local: tools }}
      initialLoading={false}
      onRefresh={vi.fn()}
    />,
  );
  const header = document.querySelector<HTMLElement>(".mobile-session-header")!;
  const capture = mockPointerCapture(header);

  fireEvent.pointerDown(header, { pointerId: 7, isPrimary: true, clientX: 100, clientY: 10 });
  fireEvent.pointerUp(header, { pointerId: 8, isPrimary: false, clientX: 0, clientY: 10 });
  fireEvent.pointerCancel(header, { pointerId: 8, isPrimary: false });

  expect(capture.releasePointerCapture).not.toHaveBeenCalled();
  expect(capture.captured).toContain(7);

  fireEvent.pointerUp(header, { pointerId: 7, isPrimary: true, clientX: 52, clientY: 10 });
  expect(capture.releasePointerCapture).toHaveBeenCalledWith(7);
  expect(screen.getByTestId("term-2")).toBeInTheDocument();
});

test("releases cancelled capture and clears a gesture when capture is lost", () => {
  render(
    <MobileSessionView
      servers={[local]}
      sessions={[session(1), session(2)]}
      toolsByServer={{ local: tools }}
      initialLoading={false}
      onRefresh={vi.fn()}
    />,
  );
  const header = document.querySelector<HTMLElement>(".mobile-session-header")!;
  const capture = mockPointerCapture(header);

  fireEvent.pointerDown(header, { pointerId: 7, isPrimary: true, clientX: 100, clientY: 10 });
  fireEvent.pointerCancel(header, { pointerId: 7, isPrimary: true });
  expect(capture.releasePointerCapture).toHaveBeenCalledWith(7);
  fireEvent.pointerUp(header, { pointerId: 7, isPrimary: true, clientX: 0, clientY: 10 });

  fireEvent.pointerDown(header, { pointerId: 8, isPrimary: true, clientX: 100, clientY: 10 });
  capture.captured.delete(8);
  fireEvent.lostPointerCapture(header, { pointerId: 8, isPrimary: true });
  fireEvent.pointerUp(header, { pointerId: 8, isPrimary: true, clientX: 0, clientY: 10 });

  expect(screen.getByTestId("term-1")).toBeInTheDocument();
});

test("ignores vertical-dominant, short, cancelled, non-primary, and mismatched-pointer gestures", () => {
  render(
    <MobileSessionView
      servers={[local]}
      sessions={[session(1), session(2)]}
      toolsByServer={{ local: tools }}
      initialLoading={false}
      onRefresh={vi.fn()}
    />,
  );
  const header = document.querySelector<HTMLElement>(".mobile-session-header")!;

  swipe(header, { toX: 51, toY: 70 });
  swipe(header, { toX: 53 });
  fireEvent.pointerDown(header, { pointerId: 1, isPrimary: true, clientX: 100, clientY: 10 });
  fireEvent.pointerCancel(header, { pointerId: 1, isPrimary: true });
  fireEvent.pointerUp(header, { pointerId: 1, isPrimary: true, clientX: 0, clientY: 10 });
  swipe(header, { toX: 0, isPrimary: false });
  fireEvent.pointerDown(header, { pointerId: 1, isPrimary: true, clientX: 100, clientY: 10 });
  fireEvent.pointerUp(header, { pointerId: 2, isPrimary: true, clientX: 0, clientY: 10 });

  expect(screen.getByTestId("term-1")).toBeInTheDocument();
  expect(screen.queryByTestId("term-2")).not.toBeInTheDocument();
});

test("changing selection unmounts the former terminal", () => {
  render(
    <MobileSessionView
      servers={[local]}
      sessions={[session(1), session(2)]}
      toolsByServer={{ local: tools }}
      initialLoading={false}
      onRefresh={vi.fn()}
    />,
  );

  swipe(document.querySelector<HTMLElement>(".mobile-session-header")!, { toX: 52 });

  expect(unmounted).toHaveBeenCalledWith(1);
  expect(screen.queryByTestId("term-1")).not.toBeInTheDocument();
  expect(screen.getByTestId("term-2")).toBeInTheDocument();
});

test("mounts the terminal key bar after Compose and targets the newly selected terminal", async () => {
  render(
    <MobileSessionView
      servers={[local]}
      sessions={[session(1), session(2)]}
      toolsByServer={{ local: tools }}
      initialLoading={false}
      onRefresh={vi.fn()}
    />,
  );
  await userEvent.click(screen.getByRole("button", { name: "Compose" }));
  const mobileTerminal = document.querySelector<HTMLElement>(".mobile-terminal")!;
  const compose = document.querySelector<HTMLElement>(".mobile-compose")!;
  const keyBar = screen.getByRole("group", { name: "Terminal keys" });

  expect(mobileTerminal.lastElementChild).toBe(keyBar);
  expect(compose.compareDocumentPosition(keyBar) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();

  fireEvent.click(within(keyBar).getByRole("button", { name: "Left" }));
  swipe(document.querySelector<HTMLElement>(".mobile-session-header")!, { toX: 52 });
  fireEvent.click(screen.getByRole("button", { name: "Right" }));

  expect(terminalCalls).toEqual([
    { sessionId: 1, operation: "input", data: "\x1b[D" },
    { sessionId: 2, operation: "input", data: "\x1b[C" },
  ]);
});

test("terminal keys preserve Compose focus and its draft", async () => {
  render(
    <MobileSessionView
      servers={[local]}
      sessions={[session(1)]}
      toolsByServer={{ local: tools }}
      initialLoading={false}
      onRefresh={vi.fn()}
    />,
  );
  await userEvent.click(screen.getByRole("button", { name: "Compose" }));
  const editor = screen.getByRole("textbox", { name: "Compose terminal input" });
  const up = screen.getByRole("button", { name: "Up" });
  fireEvent.change(editor, { target: { value: "unfinished draft" } });
  editor.focus();

  expect(fireEvent.pointerDown(up)).toBe(false);
  fireEvent.click(up);

  expect(editor).toHaveFocus();
  expect(editor).toHaveValue("unfinished draft");
  expect(terminalCalls).toEqual([{ sessionId: 1, operation: "input", data: "\x1b[A" }]);
});

test("terminal keys are a safe no-op while disconnected", () => {
  terminalConnected = false;
  render(
    <MobileSessionView
      servers={[local]}
      sessions={[session(1)]}
      toolsByServer={{ local: tools }}
      initialLoading={false}
      onRefresh={vi.fn()}
    />,
  );

  fireEvent.click(screen.getByRole("button", { name: "Esc" }));

  expect(terminalCalls).toEqual([]);
});

test("Compose opens a focused multiline editor and manual close retains its draft", async () => {
  render(
    <MobileSessionView
      servers={[local]}
      sessions={[session(1)]}
      toolsByServer={{ local: tools }}
      initialLoading={false}
      onRefresh={vi.fn()}
    />,
  );
  const toggle = screen.getByRole("button", { name: "Compose" });
  expect(toggle).toHaveAttribute("aria-expanded", "false");

  await userEvent.click(toggle);
  const editor = screen.getByRole("textbox", { name: "Compose terminal input" });
  expect(toggle).toHaveAttribute("aria-expanded", "true");
  expect(editor).toHaveFocus();
  expect(screen.queryByText("Compose terminal input")).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Add" })).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Add & Enter" })).toBeInTheDocument();
  fireEvent.change(editor, { target: { value: "draft 🌍" } });

  await userEvent.click(toggle);
  expect(screen.queryByRole("textbox", { name: "Compose terminal input" })).not.toBeInTheDocument();
  await userEvent.click(toggle);
  expect(screen.getByRole("textbox", { name: "Compose terminal input" })).toHaveValue("draft 🌍");
});

test("Add pastes exactly the draft and clears and closes Compose", async () => {
  render(
    <MobileSessionView
      servers={[local]}
      sessions={[session(1)]}
      toolsByServer={{ local: tools }}
      initialLoading={false}
      onRefresh={vi.fn()}
    />,
  );
  await userEvent.click(screen.getByRole("button", { name: "Compose" }));
  fireEvent.change(screen.getByRole("textbox", { name: "Compose terminal input" }), {
    target: { value: "first\nsecond 🐚" },
  });

  await userEvent.click(screen.getByRole("button", { name: /^Add$/ }));

  expect(terminalCalls).toEqual([{ sessionId: 1, operation: "paste", data: "first\nsecond 🐚" }]);
  expect(screen.queryByRole("textbox", { name: "Compose terminal input" })).not.toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Compose" }));
  expect(screen.getByRole("textbox", { name: "Compose terminal input" })).toHaveValue("");
});

test("Add & Enter pastes first and sends exactly one separate Enter", async () => {
  render(
    <MobileSessionView
      servers={[local]}
      sessions={[session(1)]}
      toolsByServer={{ local: tools }}
      initialLoading={false}
      onRefresh={vi.fn()}
    />,
  );
  await userEvent.click(screen.getByRole("button", { name: "Compose" }));
  fireEvent.change(screen.getByRole("textbox", { name: "Compose terminal input" }), {
    target: { value: "ship it" },
  });

  await userEvent.click(screen.getByRole("button", { name: "Add & Enter" }));

  expect(terminalCalls).toEqual([
    { sessionId: 1, operation: "paste", data: "ship it" },
    { sessionId: 1, operation: "input", data: "\r" },
  ]);
});

test("Add & Enter reports when text was inserted but Enter was not sent", async () => {
  terminalInputAccepted = false;
  render(
    <MobileSessionView
      servers={[local]}
      sessions={[session(1)]}
      toolsByServer={{ local: tools }}
      initialLoading={false}
      onRefresh={vi.fn()}
    />,
  );
  await userEvent.click(screen.getByRole("button", { name: "Compose" }));
  fireEvent.change(screen.getByRole("textbox", { name: "Compose terminal input" }), {
    target: { value: "ship it" },
  });

  await userEvent.click(screen.getByRole("button", { name: "Add & Enter" }));

  expect(terminalCalls).toEqual([{ sessionId: 1, operation: "paste", data: "ship it" }]);
  expect(screen.getByRole("textbox", { name: "Compose terminal input" })).toHaveValue("");
  expect(screen.getByRole("status")).toHaveTextContent("Text inserted, but Enter was not sent.");
});

test("a disconnected terminal preserves the Compose draft and reports it", async () => {
  terminalConnected = false;
  render(
    <MobileSessionView
      servers={[local]}
      sessions={[session(1)]}
      toolsByServer={{ local: tools }}
      initialLoading={false}
      onRefresh={vi.fn()}
    />,
  );
  await userEvent.click(screen.getByRole("button", { name: "Compose" }));
  const editor = screen.getByRole("textbox", { name: "Compose terminal input" });
  fireEvent.change(editor, { target: { value: "keep me" } });

  await userEvent.click(screen.getByRole("button", { name: /^Add$/ }));

  expect(terminalCalls).toEqual([]);
  expect(editor).toHaveValue("keep me");
  expect(screen.getByRole("status")).toHaveTextContent("Terminal is disconnected. Draft not sent.");
});

test("empty Compose actions send nothing", async () => {
  render(
    <MobileSessionView
      servers={[local]}
      sessions={[session(1)]}
      toolsByServer={{ local: tools }}
      initialLoading={false}
      onRefresh={vi.fn()}
    />,
  );
  await userEvent.click(screen.getByRole("button", { name: "Compose" }));
  expect(screen.getByRole("button", { name: /^Add$/ })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Add & Enter" })).toBeDisabled();
  expect(terminalCalls).toEqual([]);
});

test("switching sessions closes Compose and discards the former session draft", async () => {
  render(
    <MobileSessionView
      servers={[local]}
      sessions={[session(1), session(2)]}
      toolsByServer={{ local: tools }}
      initialLoading={false}
      onRefresh={vi.fn()}
    />,
  );
  await userEvent.click(screen.getByRole("button", { name: "Compose" }));
  fireEvent.change(screen.getByRole("textbox", { name: "Compose terminal input" }), {
    target: { value: "session one" },
  });

  swipe(document.querySelector<HTMLElement>(".mobile-session-header")!, { toX: 52 });

  expect(screen.getByTestId("term-2")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Compose" })).toHaveAttribute("aria-expanded", "false");
  await userEvent.click(screen.getByRole("button", { name: "Compose" }));
  expect(screen.getByRole("textbox", { name: "Compose terminal input" })).toHaveValue("");
});

test("loading and empty mobile states do not expose Compose", () => {
  const { rerender } = render(
    <MobileSessionView servers={[local]} sessions={[]} toolsByServer={{}} initialLoading onRefresh={vi.fn()} />,
  );
  expect(screen.queryByRole("button", { name: "Compose" })).not.toBeInTheDocument();
  expect(screen.queryByRole("group", { name: "Terminal keys" })).not.toBeInTheDocument();

  rerender(
    <MobileSessionView servers={[local]} sessions={[]} toolsByServer={{}} initialLoading={false} onRefresh={vi.fn()} />,
  );
  expect(screen.queryByRole("button", { name: "Compose" })).not.toBeInTheDocument();
  expect(screen.queryByRole("group", { name: "Terminal keys" })).not.toBeInTheDocument();
});
