import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";
import ToolsPanel from "../settings/ToolsPanel";

test("lists and adds tools", async () => {
  const fetchMock = vi
    .spyOn(globalThis, "fetch")
    .mockResolvedValueOnce(new Response(JSON.stringify([{ id: 1, name: "zsh", command: "zsh" }])))
    .mockResolvedValueOnce(new Response(JSON.stringify({ id: 2, name: "claude", command: "claude" }), { status: 201 }))
    .mockResolvedValueOnce(
      new Response(
        JSON.stringify([
          { id: 1, name: "zsh", command: "zsh" },
          { id: 2, name: "claude", command: "claude" },
        ]),
      ),
    );

  render(<ToolsPanel />);
  await screen.findByText((content, element) => content === "zsh" && element?.tagName === "TD");

  await userEvent.type(screen.getByPlaceholderText("name"), "claude");
  await userEvent.type(screen.getByPlaceholderText("command"), "claude");
  await userEvent.click(screen.getByText("Add tool"));

  await waitFor(() =>
    expect(
      screen.getByText((content, element) => content === "claude" && element?.tagName === "TD"),
    ).toBeInTheDocument(),
  );
  expect(fetchMock).toHaveBeenCalledTimes(3);
  fetchMock.mockRestore();
});
