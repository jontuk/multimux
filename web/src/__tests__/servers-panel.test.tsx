import { beforeEach, expect, test } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import ServersPanel from "../settings/ServersPanel";
import { listServers } from "../servers";

beforeEach(() => localStorage.clear());

async function add(origin: string, name: string) {
  await userEvent.type(screen.getByPlaceholderText("origin"), origin);
  await userEvent.type(screen.getByPlaceholderText("name"), name);
  await userEvent.click(screen.getByRole("button", { name: "Add server" }));
}

test("adding a server stores and shows its normalised origin", async () => {
  render(<ServersPanel />);
  await add("mux.local:8686/", "mux");

  expect(screen.getByText("https://mux.local:8686")).toBeInTheDocument();
  expect(listServers()[1]).toMatchObject({ origin: "https://mux.local:8686", name: "mux" });
  expect(screen.getByPlaceholderText("origin")).toHaveValue("");
});

test("an unusable origin shows an error, saves nothing, and keeps the input", async () => {
  render(<ServersPanel />);
  await add("ftp://otherbox", "bad");

  expect(screen.getByText(/not a server address/)).toBeInTheDocument();
  expect(listServers()).toHaveLength(1);
  expect(screen.getByPlaceholderText("origin")).toHaveValue("ftp://otherbox");

  // Editing the origin is the user responding to the error.
  await userEvent.type(screen.getByPlaceholderText("origin"), "x");
  expect(screen.queryByText(/not a server address/)).not.toBeInTheDocument();
});

test("a stored entry with an unusable origin is shown and can only be removed", async () => {
  localStorage.setItem("multimux.servers", JSON.stringify([{ id: "b", origin: "mux.local:8686", name: "typo" }]));
  render(<ServersPanel />);

  const row = screen.getByText("mux.local:8686").closest("tr")!;
  expect(within(row).getByText(/invalid/)).toBeInTheDocument();
  expect(within(row).queryByRole("button", { name: "Connect" })).not.toBeInTheDocument();

  await userEvent.click(within(row).getByRole("button", { name: "remove" }));
  expect(screen.queryByText("mux.local:8686")).not.toBeInTheDocument();
  expect(localStorage.getItem("multimux.servers")).toBe("[]");
});
