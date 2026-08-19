import { fireEvent, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { RefObject } from "react";
import { vi } from "vitest";
import MobileKeyBar from "../grid/MobileKeyBar";
import type { TerminalHandle } from "../term/TerminalTile";

function terminalRef(input = vi.fn()): {
  ref: RefObject<TerminalHandle | null>;
  input: ReturnType<typeof vi.fn>;
} {
  return {
    ref: {
      current: {
        input,
        paste: () => true,
        focus() {},
        setFontSize() {},
        fit() {},
      },
    },
    input,
  };
}

test("renders the essential keys in order and sends each exact terminal sequence once", async () => {
  const terminal = terminalRef();
  render(<MobileKeyBar terminalRef={terminal.ref} />);

  const keyBar = screen.getByRole("group", { name: "Terminal keys" });
  const buttons = within(keyBar).getAllByRole("button");
  expect(buttons.map((button) => button.textContent)).toEqual(["Esc", "Tab", "Ctrl-C", "←", "↑", "↓", "→", "Enter"]);
  expect(within(keyBar).getByRole("button", { name: "Left" })).toHaveTextContent("←");
  expect(within(keyBar).getByRole("button", { name: "Up" })).toHaveTextContent("↑");
  expect(within(keyBar).getByRole("button", { name: "Down" })).toHaveTextContent("↓");
  expect(within(keyBar).getByRole("button", { name: "Right" })).toHaveTextContent("→");

  for (const button of buttons) await userEvent.click(button);

  expect(terminal.input).toHaveBeenCalledTimes(8);
  expect(terminal.input.mock.calls).toEqual([
    ["\x1b"],
    ["\t"],
    ["\x03"],
    ["\x1b[D"],
    ["\x1b[A"],
    ["\x1b[B"],
    ["\x1b[C"],
    ["\r"],
  ]);
});

test("pointer activation preserves the current focus and sends one operation", () => {
  const terminal = terminalRef();
  render(
    <>
      <textarea aria-label="Compose terminal input" defaultValue="keep this draft" />
      <MobileKeyBar terminalRef={terminal.ref} />
    </>,
  );
  const editor = screen.getByRole("textbox", { name: "Compose terminal input" });
  const escape = screen.getByRole("button", { name: "Esc" });
  editor.focus();

  expect(fireEvent.pointerDown(escape)).toBe(false);
  expect(editor).toHaveFocus();
  fireEvent.click(escape);

  expect(editor).toHaveFocus();
  expect(editor).toHaveValue("keep this draft");
  expect(terminal.input).toHaveBeenCalledTimes(1);
  expect(terminal.input).toHaveBeenCalledWith("\x1b");
});
