import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import MobileSessionCreator from "../grid/MobileSessionCreator";
import type { Session } from "../grid/types";
import type { Server } from "../servers";

const local: Server = { id: "local", name: "local", origin: "https://local.test" };
const remote: Server = { id: "remote", name: "remote", origin: "https://remote.test" };

function mockLauncherDaemon(posts: Array<{ status: number; body: unknown }> = []) {
  let postIndex = 0;
  return vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const url = String(input);
    if (url.includes("/children")) return new Response(JSON.stringify(["src"]));
    if (url.includes("/subdirs")) return new Response("[]");
    if (url.includes("/api/tools")) return new Response(JSON.stringify([{ id: 4, name: "codex", command: "codex" }]));
    if (url.includes("/api/dirs")) return new Response(JSON.stringify([{ id: 7, name: "repo", path: "/repo" }]));
    if (url.includes("/api/sessions") && init?.method === "POST") {
      const next = posts[postIndex++] ?? { status: 201, body: [] };
      return new Response(JSON.stringify(next.body), { status: next.status });
    }
    return new Response("[]");
  });
}

afterEach(() => vi.restoreAllMocks());

test("shows every launch choice and opens the picker at the selected session's path", async () => {
  mockLauncherDaemon();
  render(
    <MobileSessionCreator
      servers={[local, remote]}
      initialServerId="remote"
      targetDir="/repo/web"
      targetServerId="remote"
      onCancel={vi.fn()}
      onLaunched={vi.fn()}
    />,
  );

  expect(screen.getByRole("heading", { name: "New session" })).toBeInTheDocument();
  await waitFor(() => expect(screen.getByRole("combobox", { name: "server" })).toHaveValue("remote"));
  expect(screen.getByRole("combobox", { name: "tool" })).toBeInTheDocument();
  // Location is the picker's business now — no dir select, no subdir box.
  expect(screen.queryByRole("combobox", { name: "dir" })).toBeNull();
  expect(screen.queryByRole("textbox", { name: "subdirectory" })).toBeNull();

  // Opened inside /repo/web: its children are on screen and the crumbs lead
  // back up.
  expect(await screen.findByText("src")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "web" })).toBeInTheDocument();
});

test("returns one ordered batch and does not close itself on failure", async () => {
  const onLaunched = vi.fn();
  mockLauncherDaemon([
    { status: 400, body: { error: "directory invalid" } },
    {
      status: 201,
      body: [
        { id: 41, tmuxName: "mm-41", toolId: 4, dir: "/repo", status: "running" },
        { id: 42, tmuxName: "mm-42", toolId: 4, dir: "/repo", status: "running" },
      ],
    },
  ]);
  render(<MobileSessionCreator servers={[local]} onCancel={vi.fn()} onLaunched={onLaunched} />);

  const root = await screen.findByText("repo");
  await userEvent.click(root);
  expect(await screen.findByText(/launch failed/i)).toBeInTheDocument();
  expect(screen.getByRole("heading", { name: "New session" })).toBeInTheDocument();

  await userEvent.click(screen.getByText("repo"));
  await waitFor(() => expect(onLaunched).toHaveBeenCalledTimes(1));
  expect(onLaunched.mock.calls[0][1].map((session: Session) => session.id)).toEqual([41, 42]);
});

test("Back closes without posting", async () => {
  const fetchMock = mockLauncherDaemon();
  const onCancel = vi.fn();
  render(<MobileSessionCreator servers={[local]} onCancel={onCancel} onLaunched={vi.fn()} />);
  await userEvent.click(screen.getByRole("button", { name: "Close new session creator" }));
  expect(onCancel).toHaveBeenCalledTimes(1);
  expect(fetchMock.mock.calls.some(([, init]) => init?.method === "POST")).toBe(false);
});

test("Escape closes the creator", async () => {
  mockLauncherDaemon();
  const onCancel = vi.fn();
  render(<MobileSessionCreator servers={[local]} onCancel={onCancel} onLaunched={vi.fn()} />);
  await screen.findByText("repo");
  await userEvent.keyboard("{Escape}");
  expect(onCancel).toHaveBeenCalledTimes(1);
});
