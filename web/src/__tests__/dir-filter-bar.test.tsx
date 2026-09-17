import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";
import DirFilterBar from "../grid/DirFilterBar";
import { dirTint } from "../grid/dirColor";

const dirs = [
  { path: "/Users/jon/Repos/multimux", name: "multimux", count: 3 },
  { path: "/Users/jon/old", name: "old", count: 1 },
];

const noop = () => {};

// The solo half of a pill, and the pill around it — the on/off states are
// carried by the wrapper, since they cover the close button too.
const solo = (name: RegExp) => screen.getByRole("button", { name });
const pill = (name: RegExp) => solo(name).closest(".dir-filter-item") as HTMLElement;

const MULTIMUX = /multimux 3 — show/;
const OLD = /old 1 — show/;

test("renders nothing when there are no directories", () => {
  const { container } = render(<DirFilterBar dirs={[]} solo={null} onSolo={noop} onClose={noop} />);
  expect(container).toBeEmptyDOMElement();
});

test("shows a tinted button per directory with its session count", () => {
  render(<DirFilterBar dirs={dirs} solo={null} onSolo={noop} onClose={noop} />);
  const button = solo(MULTIMUX);
  expect(button).toHaveTextContent("multimux");
  expect(button).toHaveTextContent("3");
  expect(button.title).toContain("show only sessions in /Users/jon/Repos/multimux");
  expect(pill(MULTIMUX).style.getPropertyValue("--dir-tint")).toBe(dirTint("/Users/jon/Repos/multimux"));
});

test("with no solo every button reads unpressed and undimmed", () => {
  render(<DirFilterBar dirs={dirs} solo={null} onSolo={noop} onClose={noop} />);
  for (const name of [MULTIMUX, OLD]) {
    expect(solo(name)).toHaveAttribute("aria-pressed", "false");
    expect(pill(name)).not.toHaveClass("dir-filter-off");
    expect(pill(name)).not.toHaveClass("dir-filter-on");
  }
});

test("the soloed directory reads pressed and highlighted, the rest dimmed", () => {
  render(<DirFilterBar dirs={dirs} solo="/Users/jon/old" onSolo={noop} onClose={noop} />);
  const soloed = solo(OLD);
  expect(soloed).toHaveAttribute("aria-pressed", "true");
  expect(pill(OLD)).toHaveClass("dir-filter-on");
  expect(pill(OLD)).not.toHaveClass("dir-filter-off");
  expect(soloed.title).toContain("show all directories");
  expect(soloed.title).toContain("Ctrl+Alt");

  expect(solo(MULTIMUX)).toHaveAttribute("aria-pressed", "false");
  expect(pill(MULTIMUX)).toHaveClass("dir-filter-off");
});

test("clicking a button reports that directory", async () => {
  const onSolo = vi.fn();
  render(<DirFilterBar dirs={dirs} solo={null} onSolo={onSolo} onClose={noop} />);
  await userEvent.click(solo(OLD));
  expect(onSolo).toHaveBeenCalledWith("/Users/jon/old");
});

test("each pill offers a close naming how many sessions it would end", async () => {
  const onClose = vi.fn();
  const onSolo = vi.fn();
  render(<DirFilterBar dirs={dirs} solo={null} onSolo={onSolo} onClose={onClose} />);

  // Singular and plural both read correctly.
  screen.getByRole("button", { name: "close 1 session in /Users/jon/old" });
  await userEvent.click(screen.getByRole("button", { name: "close 3 sessions in /Users/jon/Repos/multimux" }));

  expect(onClose).toHaveBeenCalledWith("/Users/jon/Repos/multimux");
  // Closing a directory is not also a request to solo it.
  expect(onSolo).not.toHaveBeenCalled();
});

test("with no filter active no + button is rendered", () => {
  render(<DirFilterBar dirs={dirs} solo={null} onSolo={noop} onAdd={noop} onClose={noop} />);
  expect(screen.queryByRole("button", { name: /\+/ })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: /show sessions in .* as well/ })).not.toBeInTheDocument();
});

test("under a solo the unselected pill offers a + button to add it, while the soloed pill does not", async () => {
  const onAdd = vi.fn();
  render(<DirFilterBar dirs={dirs} solo="/Users/jon/old" onSolo={noop} onAdd={onAdd} onClose={noop} />);

  // Soloed directory does not offer a + button.
  expect(screen.queryByRole("button", { name: "show sessions in /Users/jon/old as well" })).not.toBeInTheDocument();

  // Unselected directory offers a + button to the left of the close button.
  const addButton = screen.getByRole("button", {
    name: "show sessions in /Users/jon/Repos/multimux as well",
  });
  expect(addButton).toHaveTextContent("+");
  expect(addButton).toHaveClass("dir-filter-add");

  // DOM order: solo button, add button, close button.
  const multimuxPill = pill(MULTIMUX);
  const buttons = multimuxPill.querySelectorAll("button");
  expect(buttons).toHaveLength(3);
  expect(buttons[0]).toHaveClass("dir-filter-solo");
  expect(buttons[1]).toHaveClass("dir-filter-add");
  expect(buttons[2]).toHaveClass("dir-filter-close");

  await userEvent.click(addButton);
  expect(onAdd).toHaveBeenCalledWith("/Users/jon/Repos/multimux");
});

test("with multiple directories selected, all selected pills read on with no +, unselected pills show +", () => {
  const threeDirs = [...dirs, { path: "/Users/jon/extra", name: "extra", count: 2 }];
  render(
    <DirFilterBar
      dirs={threeDirs}
      selected={["/Users/jon/Repos/multimux", "/Users/jon/old"]}
      onSolo={noop}
      onAdd={noop}
      onClose={noop}
    />,
  );

  // Both selected pills read on, with no + button.
  expect(solo(MULTIMUX)).toHaveAttribute("aria-pressed", "true");
  expect(pill(MULTIMUX)).toHaveClass("dir-filter-on");
  expect(
    screen.queryByRole("button", { name: "show sessions in /Users/jon/Repos/multimux as well" }),
  ).not.toBeInTheDocument();

  expect(solo(OLD)).toHaveAttribute("aria-pressed", "true");
  expect(pill(OLD)).toHaveClass("dir-filter-on");
  expect(screen.queryByRole("button", { name: "show sessions in /Users/jon/old as well" })).not.toBeInTheDocument();

  // Unselected pill reads off, with a + button.
  const EXTRA = /extra 2 — show/;
  expect(solo(EXTRA)).toHaveAttribute("aria-pressed", "false");
  expect(pill(EXTRA)).toHaveClass("dir-filter-off");
  expect(screen.getByRole("button", { name: "show sessions in /Users/jon/extra as well" })).toBeInTheDocument();
});
