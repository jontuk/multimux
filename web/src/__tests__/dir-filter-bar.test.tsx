import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";
import DirFilterBar from "../grid/DirFilterBar";
import { dirTint } from "../grid/dirColor";

const dirs = [
  { path: "/Users/jon/Repos/multimux", name: "multimux", count: 3 },
  { path: "/Users/jon/old", name: "old", count: 1 },
];

test("renders nothing when there are no directories", () => {
  const { container } = render(<DirFilterBar dirs={[]} solo={null} onSolo={() => {}} />);
  expect(container).toBeEmptyDOMElement();
});

test("shows a tinted button per directory with its session count", () => {
  render(<DirFilterBar dirs={dirs} solo={null} onSolo={() => {}} />);
  const button = screen.getByRole("button", { name: /multimux/ });
  expect(button).toHaveTextContent("multimux");
  expect(button).toHaveTextContent("3");
  expect(button.title).toBe("show only sessions in /Users/jon/Repos/multimux");
  expect(button.style.getPropertyValue("--dir-tint")).toBe(dirTint("/Users/jon/Repos/multimux"));
});

test("with no solo every button reads unpressed and undimmed", () => {
  render(<DirFilterBar dirs={dirs} solo={null} onSolo={() => {}} />);
  for (const name of [/multimux/, /old/]) {
    const button = screen.getByRole("button", { name });
    expect(button).toHaveAttribute("aria-pressed", "false");
    expect(button).not.toHaveClass("dir-filter-off");
  }
});

test("the soloed directory reads pressed and the rest dimmed", () => {
  render(<DirFilterBar dirs={dirs} solo="/Users/jon/old" onSolo={() => {}} />);
  const soloed = screen.getByRole("button", { name: /old/ });
  expect(soloed).toHaveAttribute("aria-pressed", "true");
  expect(soloed).not.toHaveClass("dir-filter-off");
  expect(soloed.title).toBe("show all directories");

  const other = screen.getByRole("button", { name: /multimux/ });
  expect(other).toHaveAttribute("aria-pressed", "false");
  expect(other).toHaveClass("dir-filter-off");
});

test("clicking a button reports that directory", async () => {
  const onSolo = vi.fn();
  render(<DirFilterBar dirs={dirs} solo={null} onSolo={onSolo} />);
  await userEvent.click(screen.getByRole("button", { name: /old/ }));
  expect(onSolo).toHaveBeenCalledWith("/Users/jon/old");
});
