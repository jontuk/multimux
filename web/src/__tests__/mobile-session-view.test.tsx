import { fireEvent, render, screen } from "@testing-library/react";
import { useEffect } from "react";
import { vi } from "vitest";
import MobileSessionView from "../grid/MobileSessionView";
import type { MobileSession } from "../grid/mobileModel";
import type { Session, Tool } from "../grid/types";
import type { Server } from "../servers";

const unmounted = vi.fn();

vi.mock("../term/TerminalTile", () => ({
  default: function TerminalTileMock({ sessionId }: { sessionId: number }) {
    useEffect(() => () => unmounted(sessionId), [sessionId]);
    return <div data-testid={`term-${sessionId}`} />;
  },
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

beforeEach(() => {
  unmounted.mockClear();
});

test("distinguishes unresolved initial data from a settled empty session list", () => {
  const { rerender } = render(
    <MobileSessionView sessions={[]} toolsByServer={{}} initialLoading onRefresh={vi.fn()} />,
  );

  expect(screen.getByText("Loading sessions…")).toBeInTheDocument();
  expect(screen.queryByText(/no sessions are running/i)).not.toBeInTheDocument();

  rerender(<MobileSessionView sessions={[]} toolsByServer={{}} initialLoading={false} onRefresh={vi.fn()} />);

  expect(screen.getByText(/no sessions are running/i)).toBeInTheDocument();
  expect(screen.getByText(/launching needs a wider device/i)).toBeInTheDocument();
  expect(screen.queryByText("Loading sessions…")).not.toBeInTheDocument();
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
      sessions={[session(1)]}
      toolsByServer={{ local: tools }}
      initialLoading={false}
      onRefresh={vi.fn()}
    />,
  );
  expect(screen.getByText("#1 · claude")).toBeInTheDocument();

  rerender(
    <MobileSessionView
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

test("ignores vertical-dominant, short, cancelled, non-primary, and mismatched-pointer gestures", () => {
  render(
    <MobileSessionView
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
