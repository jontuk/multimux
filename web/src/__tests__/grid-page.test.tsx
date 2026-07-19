import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";
import GridPage from "../grid/GridPage";
import { useEvents } from "../useEvents";

vi.mock("../useEvents", () => ({ useEvents: vi.fn() }));
vi.mock("../term/TerminalTile", () => ({
  default: ({ sessionId }: { sessionId: number }) => <div data-testid={`term-${sessionId}`} />,
}));

const sessions = [
  {
    id: 1,
    tmuxName: "mm-1",
    toolId: 1,
    dir: "/a",
    status: "running",
    repoUrl: "https://github.com/org/repo",
    branch: "feat",
    gitState: "untracked",
  },
  { id: 2, tmuxName: "mm-2", toolId: 1, dir: "/b", status: "running" },
  { id: 4, tmuxName: "mm-4", toolId: 1, dir: "/c", status: "dead" },
];
const tools = [
  { id: 1, name: "claude", command: "claude" },
  { id: 2, name: "zsh", command: "zsh" },
];
const dirs = [
  { id: 1, name: "multimux", path: "/Users/jon/Repos/multimux" },
  { id: 2, name: "other", path: "/Users/jon/Repos/other" },
];

function mockFetch(layout: unknown) {
  return vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const url = String(input);
    const method = init?.method ?? "GET";
    if (url.includes("/api/layout") && method === "GET") return new Response(JSON.stringify(layout));
    if (url.includes("/api/layout") && method === "PUT") return new Response("{}");
    if (url.includes("/api/sessions") && method === "POST")
      return new Response(JSON.stringify({ id: 3, tmuxName: "mm-3", toolId: 1, dir: "/a", status: "running" }), {
        status: 201,
      });
    if (url.includes("/api/sessions")) return new Response(JSON.stringify(sessions));
    if (url.includes("/api/tools")) return new Response(JSON.stringify(tools));
    if (url.includes("/api/dirs")) return new Response(JSON.stringify(dirs));
    return new Response("[]");
  });
}

afterEach(() => {
  vi.restoreAllMocks();
});

test("attach dropdown hides sessions already placed in a tile", async () => {
  const layout = { shape: { rows: 1, cols: 2 }, tiles: [{ serverId: "local", sessionId: 1 }, null] };
  mockFetch(layout);

  render(<GridPage />);
  await screen.findByTestId("term-1");

  await screen.findByText("attach session on local…");
  const boxes = screen.getAllByRole("combobox");
  const attach = boxes.find((b) => b.textContent?.includes("attach session on local"))!;
  const options = Array.from(attach.querySelectorAll("option")).map((o) => o.textContent);
  expect(options).toContain("mm-2");
  expect(options).not.toContain("mm-1");
});

test("launcher defaults to first tool and dir, launches into first empty tile", async () => {
  const layout = { shape: { rows: 1, cols: 2 }, tiles: [null, null] };
  const fetchMock = mockFetch(layout);

  render(<GridPage />);

  const toolSelect = await screen.findByLabelText<HTMLSelectElement>("tool");
  const dirSelect = screen.getByLabelText<HTMLSelectElement>("dir");
  await waitFor(() => expect(toolSelect.value).toBe("1"));
  expect(dirSelect.value).toBe("1");

  await userEvent.click(screen.getByText("+ New"));
  await screen.findByTestId("term-3");

  const post = fetchMock.mock.calls.find(([, init]) => init?.method === "POST");
  expect(post).toBeTruthy();
  expect(JSON.parse(String(post?.[1]?.body))).toEqual({ toolId: 1, dirId: 1 });
});

test("launching when grid is full grows the grid instead of blocking", async () => {
  const layout = { shape: { rows: 1, cols: 1 }, tiles: [{ serverId: "local", sessionId: 1 }] };
  const fetchMock = mockFetch(layout);

  render(<GridPage />);
  await screen.findByTestId("term-1");

  const button = await screen.findByText<HTMLButtonElement>("+ New");
  await waitFor(() => expect(button).toBeEnabled());
  await userEvent.click(button);
  await screen.findByTestId("term-3");
  expect(screen.getByTestId("term-1")).toBeInTheDocument();

  const put = fetchMock.mock.calls.findLast(([, init]) => init?.method === "PUT");
  const saved = JSON.parse(String(put?.[1]?.body));
  expect(saved.shape).toEqual({ rows: 2, cols: 1 });
});

test("tile header shows session id, tool, dir, and remove-from-grid keeps the session alive", async () => {
  const layout = { shape: { rows: 1, cols: 2 }, tiles: [{ serverId: "local", sessionId: 1 }, null] };
  const fetchMock = mockFetch(layout);

  render(<GridPage />);
  await screen.findByTestId("term-1");

  await screen.findByText("#1 · claude");
  expect(screen.getByTitle("/a")).toBeInTheDocument();

  await userEvent.click(screen.getByLabelText("remove session 1 from grid"));
  expect(screen.queryByTestId("term-1")).not.toBeInTheDocument();
  expect(fetchMock.mock.calls.some(([, init]) => init?.method === "DELETE")).toBe(false);
});

test("tile header links to GitHub when the session dir has a repoUrl", async () => {
  const layout = {
    shape: { rows: 1, cols: 2 },
    tiles: [
      { serverId: "local", sessionId: 1 },
      { serverId: "local", sessionId: 2 },
    ],
  };
  mockFetch(layout);

  render(<GridPage />);
  await screen.findByTestId("term-1");
  await screen.findByTestId("term-2");

  // Session 1 has a repoUrl; session 2 does not.
  const link = await screen.findByRole<HTMLAnchorElement>("link", { name: "open repository on GitHub" });
  expect(link.href).toBe("https://github.com/org/repo");
  expect(link.target).toBe("_blank");
  expect(screen.getAllByRole("link", { name: "open repository on GitHub" })).toHaveLength(1);
});

test("tile header shows branch name and git state dot when the session dir is a repo", async () => {
  const layout = {
    shape: { rows: 1, cols: 2 },
    tiles: [
      { serverId: "local", sessionId: 1 },
      { serverId: "local", sessionId: 2 },
    ],
  };
  mockFetch(layout);

  render(<GridPage />);
  await screen.findByTestId("term-1");
  await screen.findByTestId("term-2");

  // Session 1 is a repo on branch "feat" with untracked files; session 2 is not a repo.
  const branch = await screen.findByText("feat");
  expect(branch.className).toContain("tile-branch");
  const dot = screen.getByTitle("untracked files present");
  expect(dot.className).toContain("git-dot-untracked");
  expect(screen.getAllByText("feat")).toHaveLength(1);
});

test("git_changed event refetches sessions", async () => {
  const layout = { shape: { rows: 1, cols: 1 }, tiles: [{ serverId: "local", sessionId: 1 }] };
  const fetchMock = mockFetch(layout);

  render(<GridPage />);
  await screen.findByTestId("term-1");

  const sessionFetches = () => fetchMock.mock.calls.filter(([input]) => String(input).includes("/api/sessions")).length;
  const before = sessionFetches();

  const calls = vi.mocked(useEvents).mock.calls;
  const onEvent = calls[calls.length - 1][1];
  act(() => onEvent("git_changed"));

  await waitFor(() => expect(sessionFetches()).toBeGreaterThan(before));
});

test("terminate button confirms then DELETEs the session and drops the tile", async () => {
  const layout = { shape: { rows: 1, cols: 2 }, tiles: [{ serverId: "local", sessionId: 1 }, null] };
  const fetchMock = mockFetch(layout);
  vi.spyOn(window, "confirm").mockReturnValue(true);

  render(<GridPage />);
  await screen.findByTestId("term-1");

  await userEvent.click(screen.getByLabelText("terminate session 1"));
  await waitFor(() => expect(screen.queryByTestId("term-1")).not.toBeInTheDocument());
  const delCall = fetchMock.mock.calls.find(([, init]) => init?.method === "DELETE");
  expect(String(delCall?.[0])).toContain("/api/sessions/1");
});

test("header offers quick-add buttons for sessions not in the grid", async () => {
  const layout = { shape: { rows: 1, cols: 2 }, tiles: [{ serverId: "local", sessionId: 1 }, null] };
  mockFetch(layout);

  render(<GridPage />);
  await screen.findByTestId("term-1");

  // Session 2 is running but unplaced; session 1 is already in the grid.
  const quickAdd = await screen.findByText("+ #2 claude");
  expect(screen.queryByText("+ #1 claude")).not.toBeInTheDocument();

  await userEvent.click(quickAdd);
  await screen.findByTestId("term-2");
  expect(screen.queryByText("+ #2 claude")).not.toBeInTheDocument();
});

test("dead sessions are not offered for re-adding (quick-add or attach dropdown)", async () => {
  const layout = { shape: { rows: 1, cols: 2 }, tiles: [{ serverId: "local", sessionId: 1 }, null] };
  mockFetch(layout);

  render(<GridPage />);
  await screen.findByTestId("term-1");
  await screen.findByText("+ #2 claude");

  // Session 4 is dead: no quick-add button, not in the empty-tile dropdown.
  expect(screen.queryByText("+ #4 claude")).not.toBeInTheDocument();
  const attach = screen.getAllByRole("combobox").find((b) => b.textContent?.includes("attach session on local"))!;
  const options = Array.from(attach.querySelectorAll("option")).map((o) => o.textContent);
  expect(options).not.toContain("mm-4");
});

test("tile for a removed server shows a non-interactive state, never the local daemon", async () => {
  const layout = {
    shape: { rows: 1, cols: 2 },
    tiles: [{ serverId: "gone-server-id", sessionId: 1 }, null],
  };
  const fetchMock = mockFetch(layout);
  vi.spyOn(window, "confirm").mockReturnValue(true);

  render(<GridPage />);
  await screen.findByText(/server removed/i);

  // No terminal may attach: local session #1 must not be shown for the orphaned tile.
  expect(screen.queryByTestId("term-1")).not.toBeInTheDocument();
  // No terminate action: it would DELETE local session #1.
  expect(screen.queryByLabelText("terminate session 1")).not.toBeInTheDocument();
  expect(fetchMock.mock.calls.some(([, init]) => init?.method === "DELETE")).toBe(false);

  // Removing the orphaned tile from the grid is still allowed.
  await userEvent.click(screen.getByLabelText("remove session 1 from grid"));
  expect(screen.queryByText(/server removed/i)).not.toBeInTheDocument();
});

test("double-clicking a tile header maximizes the tile; double-clicking again restores", async () => {
  const layout = {
    shape: { rows: 1, cols: 2 },
    tiles: [
      { serverId: "local", sessionId: 1 },
      { serverId: "local", sessionId: 2 },
    ],
  };
  mockFetch(layout);

  render(<GridPage />);
  await screen.findByTestId("term-1");

  const header = screen.getByText("#1 · claude").closest(".tile-header")!;
  const tile = header.closest(".tile")!;

  await userEvent.dblClick(header);
  expect(tile.className).toContain("tile-maximized");

  await userEvent.dblClick(header);
  expect(tile.className).not.toContain("tile-maximized");
});

test("Escape restores the grid while a tile is maximized", async () => {
  const layout = { shape: { rows: 1, cols: 1 }, tiles: [{ serverId: "local", sessionId: 1 }] };
  mockFetch(layout);

  render(<GridPage />);
  await screen.findByTestId("term-1");

  const header = screen.getByText("#1 · claude").closest(".tile-header")!;
  const tile = header.closest(".tile")!;

  await userEvent.dblClick(header);
  expect(tile.className).toContain("tile-maximized");

  await userEvent.keyboard("{Escape}");
  expect(tile.className).not.toContain("tile-maximized");
});

test("removing the maximized tile clears maximized state", async () => {
  const layout = { shape: { rows: 1, cols: 2 }, tiles: [{ serverId: "local", sessionId: 1 }, null] };
  mockFetch(layout);

  render(<GridPage />);
  await screen.findByTestId("term-1");

  const header = screen.getByText("#1 · claude").closest(".tile-header")!;
  await userEvent.dblClick(header);
  expect(document.querySelector(".tile-maximized")).not.toBeNull();

  await userEvent.click(screen.getByLabelText("remove session 1 from grid"));
  expect(document.querySelector(".tile-maximized")).toBeNull();

  // Re-add the same session: it must come back un-maximized.
  await userEvent.click(await screen.findByText("+ #1 claude"));
  await screen.findByTestId("term-1");
  expect(document.querySelector(".tile-maximized")).toBeNull();
});

test("dead session tile shows ended state instead of mounting a terminal", async () => {
  // Session 4 is dead in the fixtures above.
  const layout = { shape: { rows: 1, cols: 2 }, tiles: [{ serverId: "local", sessionId: 4 }, null] };
  mockFetch(layout);

  render(<GridPage />);
  await screen.findByText(/session ended/);
  expect(screen.queryByTestId("term-4")).toBeNull();
});

function stubRemoteServer() {
  localStorage.setItem(
    "multimux.servers",
    JSON.stringify([{ id: "r1", origin: "https://box-a:8686", name: "box-a", token: "dead" }]),
  );
}

function remoteOnStatus() {
  const calls = vi.mocked(useEvents).mock.calls.filter(([s]) => s.id === "r1");
  return calls[calls.length - 1][2]!;
}

test("expired remote auth offers Reconnect that replaces the stored token", async () => {
  stubRemoteServer();
  const layout = { shape: { rows: 1, cols: 2 }, tiles: [null, null] };
  mockFetch(layout);
  const popup = { closed: false, close: vi.fn() };
  const openSpy = vi.spyOn(window, "open").mockReturnValue(popup as unknown as Window);

  try {
    render(<GridPage />);
    act(() => remoteOnStatus()("auth-expired"));
    await screen.findByText(/not logged in/);

    await userEvent.click(screen.getByText("Reconnect"));
    expect(String(openSpy.mock.calls[0][0])).toContain("https://box-a:8686/#/connect");

    act(() => {
      window.dispatchEvent(
        new MessageEvent("message", {
          origin: "https://box-a:8686",
          data: { type: "multimux-token", token: "fresh" },
        }),
      );
    });
    await waitFor(() => expect(JSON.parse(localStorage.getItem("multimux.servers")!)[0].token).toBe("fresh"));
  } finally {
    localStorage.removeItem("multimux.servers");
  }
});

test("expired remote auth offers Remove server as a way out", async () => {
  stubRemoteServer();
  const layout = { shape: { rows: 1, cols: 2 }, tiles: [null, null] };
  mockFetch(layout);

  try {
    render(<GridPage />);
    act(() => remoteOnStatus()("auth-expired"));
    await screen.findByText(/not logged in/);

    await userEvent.click(screen.getByText("Remove server"));
    expect(JSON.parse(localStorage.getItem("multimux.servers")!)).toEqual([]);
    expect(screen.queryByText(/not logged in/)).not.toBeInTheDocument();
  } finally {
    localStorage.removeItem("multimux.servers");
  }
});

test("layout persistence keeps one PUT in flight and coalesces to the newest state", async () => {
  const layout = { shape: { rows: 1, cols: 2 }, tiles: [null, null] };
  const putBodies: { shape: { rows: number; cols: number } }[] = [];
  const putResolvers: Array<() => void> = [];
  let putsInFlight = 0;
  let maxPutsInFlight = 0;
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const url = String(input);
    const method = init?.method ?? "GET";
    if (url.includes("/api/layout") && method === "PUT") {
      putBodies.push(JSON.parse(String(init?.body)));
      putsInFlight++;
      maxPutsInFlight = Math.max(maxPutsInFlight, putsInFlight);
      await new Promise<void>((resolve) => putResolvers.push(resolve));
      putsInFlight--;
      return new Response("{}");
    }
    if (url.includes("/api/layout")) return new Response(JSON.stringify(layout));
    if (url.includes("/api/sessions")) return new Response(JSON.stringify(sessions));
    if (url.includes("/api/tools")) return new Response(JSON.stringify(tools));
    if (url.includes("/api/dirs")) return new Response(JSON.stringify(dirs));
    return new Response("[]");
  });

  render(<GridPage />);
  const more = await screen.findByLabelText("more columns");

  // Two rapid edits: the first PUT (cols 3) is held open; the second edit must
  // queue, not fire a concurrent PUT that could land out of order.
  await userEvent.click(more);
  await userEvent.click(more);
  expect(putBodies).toHaveLength(1);
  expect(putBodies[0].shape).toEqual({ rows: 1, cols: 3 });

  // Releasing the first PUT flushes exactly one follow-up with the newest state.
  putResolvers.shift()!();
  await waitFor(() => expect(putBodies).toHaveLength(2));
  expect(putBodies[1].shape).toEqual({ rows: 1, cols: 4 });
  expect(maxPutsInFlight).toBe(1);

  putResolvers.shift()!();
  await waitFor(() => expect(putsInFlight).toBe(0));
  expect(putBodies).toHaveLength(2);
});

// Minimal stand-in for jsdom's missing DataTransfer.
function makeDataTransfer(data: Record<string, string> = {}) {
  return {
    data,
    types: Object.keys(data),
    setData(type: string, value: string) {
      this.data[type] = value;
      this.types = Object.keys(this.data);
    },
    getData(type: string) {
      return this.data[type] ?? "";
    },
  };
}

const twoTileLayout = {
  shape: { rows: 1, cols: 2 },
  tiles: [
    { serverId: "local", sessionId: 1 },
    { serverId: "local", sessionId: 2 },
  ],
};

test("dragging a tile onto another swaps them without remounting the terminals", async () => {
  mockFetch(twoTileLayout);

  render(<GridPage />);
  const term1 = await screen.findByTestId("term-1");
  await screen.findByTestId("term-2");
  const tiles = document.querySelectorAll(".tile");

  const dt = makeDataTransfer();
  fireEvent.dragStart(tiles[0], { dataTransfer: dt });
  fireEvent.drop(tiles[1], { dataTransfer: dt });

  // Order swapped…
  const after = document.querySelectorAll(".tile");
  expect(after[0].querySelector("[data-testid=term-2]")).not.toBeNull();
  // …and the terminal kept its DOM node (identity key, not index key), so
  // xterm and its WebSocket survive the move.
  expect(screen.getByTestId("term-1")).toBe(term1);
});

test("drops without the tile MIME type are ignored (no swap of tile zero)", async () => {
  const fetchMock = mockFetch(twoTileLayout);

  render(<GridPage />);
  await screen.findByTestId("term-1");
  const tiles = document.querySelectorAll(".tile");

  // A foreign drag (text, file, …) carries no tile index; Number("") === 0
  // must not be treated as "swap with tile 0".
  fireEvent.drop(tiles[1], { dataTransfer: makeDataTransfer({ "text/plain": "hello" }) });

  expect(document.querySelectorAll(".tile")[0].querySelector("[data-testid=term-1]")).not.toBeNull();
  expect(fetchMock.mock.calls.some(([, init]) => init?.method === "PUT")).toBe(false);
});

test("drops with an out-of-range tile index are ignored", async () => {
  const fetchMock = mockFetch(twoTileLayout);

  render(<GridPage />);
  await screen.findByTestId("term-1");
  const tiles = document.querySelectorAll(".tile");

  fireEvent.drop(tiles[1], { dataTransfer: makeDataTransfer({ "text/tile-index": "99" }) });

  expect(fetchMock.mock.calls.some(([, init]) => init?.method === "PUT")).toBe(false);
});

test("move button offers tap targets that reorder tiles without drag", async () => {
  const fetchMock = mockFetch(twoTileLayout);

  render(<GridPage />);
  await screen.findByTestId("term-1");
  await screen.findByTestId("term-2");

  // Tap "move" on tile #2, then tap the target that appears on the other cell.
  await userEvent.click(screen.getByLabelText("move session 2"));
  await userEvent.click(screen.getByLabelText("move here"));

  expect(document.querySelectorAll(".tile")[0].querySelector("[data-testid=term-2]")).not.toBeNull();
  const put = fetchMock.mock.calls.findLast(([, init]) => init?.method === "PUT");
  expect(JSON.parse(String(put?.[1]?.body)).tiles).toEqual([
    { serverId: "local", sessionId: 2 },
    { serverId: "local", sessionId: 1 },
  ]);
  // Move mode ends once the move completes.
  expect(screen.queryByLabelText("move here")).toBeNull();
});

test("tapping move again cancels move mode", async () => {
  mockFetch(twoTileLayout);

  render(<GridPage />);
  await screen.findByTestId("term-1");

  await userEvent.click(screen.getByLabelText("move session 1"));
  expect(screen.getByLabelText("move here")).toBeInTheDocument();

  await userEvent.click(screen.getByLabelText("move session 1"));
  expect(screen.queryByLabelText("move here")).toBeNull();
});

test("stepper arrows change column count and persist it", async () => {
  const layout = { shape: { rows: 1, cols: 2 }, tiles: [null, null] };
  const fetchMock = mockFetch(layout);

  render(<GridPage />);
  await screen.findByLabelText("more columns");

  await userEvent.click(screen.getByLabelText("more columns"));
  let put = fetchMock.mock.calls.findLast(([, init]) => init?.method === "PUT");
  expect(JSON.parse(String(put?.[1]?.body)).shape).toEqual({ rows: 1, cols: 3 });

  await userEvent.click(screen.getByLabelText("fewer columns"));
  put = fetchMock.mock.calls.findLast(([, init]) => init?.method === "PUT");
  expect(JSON.parse(String(put?.[1]?.body)).shape).toEqual({ rows: 1, cols: 2 });
});
