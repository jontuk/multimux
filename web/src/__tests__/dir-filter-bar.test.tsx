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
  const { container } = render(<DirFilterBar dirs={[]} hidden={new Set()} onToggle={() => {}} />);
  expect(container).toBeEmptyDOMElement();
});

test("shows a tinted button per directory with its session count", () => {
  render(<DirFilterBar dirs={dirs} hidden={new Set()} onToggle={() => {}} />);
  const button = screen.getByRole("button", { name: /multimux/ });
  expect(button).toHaveTextContent("multimux");
  expect(button).toHaveTextContent("3");
  expect(button.title).toContain("/Users/jon/Repos/multimux");
  expect(button.style.getPropertyValue("--dir-tint")).toBe(dirTint("/Users/jon/Repos/multimux"));
});

test("a hidden directory reads as unpressed", () => {
  render(<DirFilterBar dirs={dirs} hidden={new Set(["/Users/jon/old"])} onToggle={() => {}} />);
  expect(screen.getByRole("button", { name: /multimux/ })).toHaveAttribute("aria-pressed", "true");
  expect(screen.getByRole("button", { name: /old/ })).toHaveAttribute("aria-pressed", "false");
});

test("clicking a button toggles that directory", async () => {
  const onToggle = vi.fn();
  render(<DirFilterBar dirs={dirs} hidden={new Set()} onToggle={onToggle} />);
  await userEvent.click(screen.getByRole("button", { name: /old/ }));
  expect(onToggle).toHaveBeenCalledWith("/Users/jon/old");
});
