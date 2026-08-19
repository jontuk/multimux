import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";
import GridPage from "../grid/GridPage";
import { dirTint } from "../grid/dirColor";
import { soloDir } from "../grid/dirFilter";
import { useEvents } from "../useEvents";
import { MOBILE_VIEW_QUERY } from "../useMediaQuery";
import { endReflowHold, isReflowHeld } from "../term/reflowGate";

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
    ahead: 2,
    behind: 1,
  },
  { id: 2, tmuxName: "mm-2", toolId: 1, dir: "/b", status: "running", label: "api refactor" },
  { id: 4, tmuxName: "mm-4", toolId: 1, dir: "/c", status: "dead" },
  {
    id: 5,
    tmuxName: "mm-5",
    toolId: 1,
    dir: "/d",
    status: "running",
    branch: "wip",
    gitState: "clean",
    noUpstream: true,
  },
];
const tools = [
  { id: 1, name: "claude", command: "claude" },
  { id: 2, name: "zsh", command: "zsh" },
];
const dirs = [
  { id: 1, name: "multimux", path: "/Users/jon/Repos/multimux" },
  { id: 2, name: "other", path: "/Users/jon/Repos/other" },
];

function mockFetch(layout: unknown, sessionList: unknown[] = sessions) {
  return vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const url = String(input);
    const method = init?.method ?? "GET";
    if (url.includes("/subdirs")) return new Response("[]");
    if (url.includes("/api/layout") && method === "GET") return new Response(JSON.stringify(layout));
    if (url.includes("/api/layout") && method === "PUT") return new Response("{}");
    if (url.includes("/label") && method === "PUT")
      return new Response(JSON.stringify({ id: 1, tmuxName: "mm-1", toolId: 1, dir: "/a", status: "running" }));
    if (url.includes("/api/sessions") && method === "POST")
      return new Response(JSON.stringify([{ id: 3, tmuxName: "mm-3", toolId: 1, dir: "/a", status: "running" }]), {
        status: 201,
      });
    if (url.includes("/api/sessions")) return new Response(JSON.stringify(sessionList));
    if (url.includes("/api/tools")) return new Response(JSON.stringify(tools));
    if (url.includes("/api/dirs")) return new Response(JSON.stringify(dirs));
    return new Response("[]");
  });
}

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  localStorage.removeItem("multimux.servers");
  localStorage.clear();
  // A failed or interrupted drag test must not leave the module-level reflow
  // gate held for other tests in this file.
  if (isReflowHeld()) endReflowHold();
});

function stubMatchMedia(initialMatches: boolean) {
  let matches = initialMatches;
  const listeners = new Set<() => void>();
  const media = {
    get matches() {
      return matches;
    },
    media: MOBILE_VIEW_QUERY,
    onchange: null,
    addEventListener: (_type: string, listener: () => void) => listeners.add(listener),
    removeEventListener: (_type: string, listener: () => void) => listeners.delete(listener),
    addListener: () => {},
    removeListener: () => {},
    dispatchEvent: () => true,
  };
  vi.stubGlobal(
    "matchMedia",
    vi.fn(() => media),
  );
  return {
    setMatches(next: boolean) {
      matches = next;
      act(() => listeners.forEach((listener) => listener()));
    },
  };
}

test("wide mode keeps the launcher, grid, empty cells, and tile actions", async () => {
  stubMatchMedia(false);
  const layout = { shape: { rows: 1, cols: 2 }, tiles: [{ serverId: "local", sessionId: 1 }, null] };
  mockFetch(layout);

  render(<GridPage />);

  await screen.findByText("+ New");
  expect(document.querySelector(".grid")).not.toBeNull();
  expect(document.querySelector(".empty-tile")).not.toBeNull();
  expect(screen.getByLabelText("remove session 1 from grid")).toBeInTheDocument();
  expect(screen.getByLabelText("terminate session 1")).toBeInTheDocument();
});

test("narrow mode renders the mobile session view without desktop controls", async () => {
  stubMatchMedia(true);
  const layout = { shape: { rows: 1, cols: 2 }, tiles: [{ serverId: "local", sessionId: 1 }, null] };
  mockFetch(layout);

  render(<GridPage hostLabel="work-mac" accentColor="#3fb950" />);

  await screen.findByText("1/3");
  const mobileHeader = document.querySelector<HTMLElement>(".mobile-session-header")!;
  expect(document.querySelector(".mobile-session-view")).not.toBeNull();
  expect(mobileHeader).toHaveTextContent("@work-mac");
  expect(mobileHeader).toHaveClass("host-accented");
  expect(mobileHeader.style.getPropertyValue("--host-accent")).toBe("#3fb950");
  expect(document.querySelector(".grid")).toBeNull();
  expect(document.querySelector(".empty-tile")).toBeNull();
  expect(screen.queryByText("+ New")).not.toBeInTheDocument();
  expect(screen.queryByLabelText("more columns")).not.toBeInTheDocument();
  expect(screen.queryByLabelText("remove session 1 from grid")).not.toBeInTheDocument();
  expect(screen.queryByLabelText("terminate session 1")).not.toBeInTheDocument();
});

test("crossing the mobile breakpoint switches branches without persisting layout", async () => {
  const media = stubMatchMedia(false);
  const layout = { shape: { rows: 1, cols: 2 }, tiles: [{ serverId: "local", sessionId: 1 }, null] };
  const fetchMock = mockFetch(layout);

  render(<GridPage />);
  await screen.findByText("+ New");

  media.setMatches(true);
  await screen.findByText("1/3");
  expect(screen.queryByText("+ New")).not.toBeInTheDocument();

  media.setMatches(false);
  await screen.findByText("+ New");
  expect(
    fetchMock.mock.calls.some(([url, init]) => String(url).includes("/api/layout") && init?.method === "PUT"),
  ).toBe(false);
});

test("narrow empty state waits for layout and every configured server session request to settle", async () => {
  stubMatchMedia(true);
  stubRemoteServer();
  const pending = new Map<string, (response: Response) => void>();
  vi.spyOn(globalThis, "fetch").mockImplementation((input) => {
    const url = String(input);
    if (url.includes("/api/layout") || url.includes("/api/sessions")) {
      return new Promise<Response>((resolve) => pending.set(url, resolve));
    }
    return Promise.resolve(new Response("[]"));
  });

  render(<GridPage />);
  expect(screen.getByText("Loading sessions…")).toBeInTheDocument();
  expect(screen.queryByText("No sessions are running.")).not.toBeInTheDocument();

  await act(async () => {
    pending.get(`${window.location.origin}/api/layout`)!(new Response(JSON.stringify(emptyLayoutFixture)));
  });
  await waitFor(() => expect(screen.getByText("Loading sessions…")).toBeInTheDocument());

  await act(async () => {
    pending.get(`${window.location.origin}/api/sessions`)!(new Response("[]"));
  });
  await waitFor(() => expect(screen.getByText("Loading sessions…")).toBeInTheDocument());

  await act(async () => {
    pending.get("https://box-a:8686/api/sessions")!(new Response("[]"));
  });
  expect(await screen.findByText("No sessions are running.")).toBeInTheDocument();
});

test("a rejected server request settles while its banner coexists with a reachable mobile terminal", async () => {
  stubMatchMedia(true);
  stubRemoteServer();
  const layout = { shape: { rows: 1, cols: 1 }, tiles: [{ serverId: "local", sessionId: 1 }] };
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const url = String(input);
    if (url.startsWith("https://box-a:8686") && url.includes("/api/sessions")) throw new Error("offline");
    if (url.includes("/subdirs")) return new Response("[]");
    if (url.includes("/api/layout")) return new Response(JSON.stringify(layout));
    if (url.includes("/api/sessions")) return new Response(JSON.stringify([sessions[0]]));
    if (url.includes("/api/tools")) return new Response(JSON.stringify(tools));
    if (url.includes("/api/dirs")) return new Response(JSON.stringify(dirs));
    return new Response("[]");
  });

  render(<GridPage />);
  await screen.findByTestId("term-1");
  expect(document.querySelector(".mobile-session-view")).not.toBeNull();
  expect(screen.queryByText("Loading sessions…")).not.toBeInTheDocument();

  act(() => remoteOnStatus()("unreachable"));
  expect(await screen.findByText(/daemon unreachable/)).toBeInTheDocument();
  expect(screen.getByTestId("term-1")).toBeInTheDocument();
});

const emptyLayoutFixture = { shape: { rows: 1, cols: 1 }, tiles: [null] };

test("attach dropdown hides sessions already placed in a tile", async () => {
  const layout = { shape: { rows: 1, cols: 2 }, tiles: [{ serverId: "local", sessionId: 1 }, null] };
  mockFetch(layout);

  render(<GridPage />);
  await screen.findByTestId("term-1");

  await screen.findByText("attach session on local…");
  const boxes = screen.getAllByRole("combobox");
  const attach = boxes.find((b) => b.textContent?.includes("attach session on local"))!;
  const options = Array.from(attach.querySelectorAll("option")).map((o) => o.textContent);
  expect(options).toContain("api refactor");
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
  expect(JSON.parse(String(post?.[1]?.body))).toEqual({ toolId: 1, dirId: 1, subdir: "" });
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
  // A full one-column grid widens rather than stacking, so the pair sits side by side.
  expect(saved.shape).toEqual({ rows: 1, cols: 2 });
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

test("tile header shows ahead/behind counts and marks a never-pushed branch", async () => {
  const layout = {
    shape: { rows: 1, cols: 2 },
    tiles: [
      { serverId: "local", sessionId: 1 },
      { serverId: "local", sessionId: 5 },
    ],
  };
  mockFetch(layout);

  render(<GridPage />);
  await screen.findByTestId("term-1");
  await screen.findByTestId("term-5");

  // Session 1 is 2 ahead / 1 behind its upstream.
  expect(await screen.findByText("↑2")).toBeTruthy();
  expect(screen.getByText("↓1")).toBeTruthy();
  // Session 5 has commits but no upstream at all.
  const unpushed = screen.getByTitle("branch has never been pushed");
  expect(unpushed.textContent).toBe("↑?");
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

test("hello event (socket reconnect) refetches sessions and layout", async () => {
  // The hub drops events for slow subscribers, so a reconnected socket must
  // resync from scratch; the server sends "hello" on every (re)connect.
  const layout = { shape: { rows: 1, cols: 1 }, tiles: [{ serverId: "local", sessionId: 1 }] };
  const fetchMock = mockFetch(layout);

  render(<GridPage />);
  await screen.findByTestId("term-1");

  const gets = (path: string) =>
    fetchMock.mock.calls.filter(([input, init]) => String(input).includes(path) && (init?.method ?? "GET") === "GET")
      .length;
  const sessionsBefore = gets("/api/sessions");
  const layoutBefore = gets("/api/layout");

  const calls = vi.mocked(useEvents).mock.calls;
  const onEvent = calls[calls.length - 1][1];
  act(() => onEvent("hello"));

  await waitFor(() => expect(gets("/api/sessions")).toBeGreaterThan(sessionsBefore));
  await waitFor(() => expect(gets("/api/layout")).toBeGreaterThan(layoutBefore));
});

test("terminate skips the confirm prompt by default", async () => {
  const layout = { shape: { rows: 1, cols: 2 }, tiles: [{ serverId: "local", sessionId: 1 }, null] };
  const fetchMock = mockFetch(layout);
  const confirmMock = vi.spyOn(window, "confirm").mockReturnValue(true);

  render(<GridPage />);
  await screen.findByTestId("term-1");

  await userEvent.click(screen.getByLabelText("terminate session 1"));
  await waitFor(() => expect(screen.queryByTestId("term-1")).not.toBeInTheDocument());
  expect(confirmMock).not.toHaveBeenCalled();
  const delCall = fetchMock.mock.calls.find(([, init]) => init?.method === "DELETE");
  expect(String(delCall?.[0])).toContain("/api/sessions/1");
});

test("terminate confirms first when confirmTerminate is on", async () => {
  const layout = { shape: { rows: 1, cols: 2 }, tiles: [{ serverId: "local", sessionId: 1 }, null] };
  const fetchMock = mockFetch(layout);
  const confirmMock = vi.spyOn(window, "confirm").mockReturnValue(false);

  render(<GridPage confirmTerminate />);
  await screen.findByTestId("term-1");

  // Declining leaves the session alone.
  await userEvent.click(screen.getByLabelText("terminate session 1"));
  expect(confirmMock).toHaveBeenCalledTimes(1);
  expect(screen.getByTestId("term-1")).toBeInTheDocument();
  expect(fetchMock.mock.calls.some(([, init]) => init?.method === "DELETE")).toBe(false);

  // Accepting goes through.
  confirmMock.mockReturnValue(true);
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

  // Session 2 is running but unplaced (and labelled); session 1 is already in the grid.
  const quickAdd = await screen.findByText("+ #2 api refactor");
  expect(screen.queryByText("+ #1 claude")).not.toBeInTheDocument();

  await userEvent.click(quickAdd);
  await screen.findByTestId("term-2");
  expect(screen.queryByText("+ #2 api refactor")).not.toBeInTheDocument();
});

test("dead sessions are not offered for re-adding (quick-add or attach dropdown)", async () => {
  const layout = { shape: { rows: 1, cols: 2 }, tiles: [{ serverId: "local", sessionId: 1 }, null] };
  mockFetch(layout);

  render(<GridPage />);
  await screen.findByTestId("term-1");
  await screen.findByText("+ #2 api refactor");

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

test("tile headers carry a --dir-tint hashed from the session's directory", async () => {
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

  const header = (id: string) => screen.getByText(id).closest(".tile-header") as HTMLElement;
  // Session #1 is in /a, #2 in /b: same dir means the same tint, so different
  // dirs must not collide.
  expect(header("#1 · claude").style.getPropertyValue("--dir-tint")).toBe(dirTint("/a"));
  expect(header("#2 · api refactor").style.getPropertyValue("--dir-tint")).toBe(dirTint("/b"));
  expect(dirTint("/a")).not.toBe(dirTint("/b"));
});

test("a tile whose server was removed gets no dir tint", async () => {
  const layout = { shape: { rows: 1, cols: 1 }, tiles: [{ serverId: "gone", sessionId: 1 }] };
  mockFetch(layout);

  render(<GridPage />);
  const header = (await screen.findByText("#1 · server removed")).closest(".tile-header") as HTMLElement;
  expect(header.style.getPropertyValue("--dir-tint")).toBe("");
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
    if (url.includes("/subdirs")) return new Response("[]");
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

test("a labelled session shows its label in the tile title", async () => {
  const layout = { shape: { rows: 1, cols: 2 }, tiles: [{ serverId: "local", sessionId: 2 }, null] };
  mockFetch(layout);

  render(<GridPage />);

  expect(await screen.findByText("#2 · api refactor")).toBeTruthy();
});

// A placed session never appears in the attach dropdown (existing invariant:
// each session may only be open in one tile), so this exercises an *unplaced*
// labelled session instead.
test("a labelled session shows its label, not its tmux name, in the attach dropdown", async () => {
  const layout = { shape: { rows: 1, cols: 2 }, tiles: [null, null] };
  mockFetch(layout);

  render(<GridPage />);

  await screen.findAllByText("attach session on local…");
  const attach = screen.getAllByRole("combobox").find((b) => b.textContent?.includes("attach session on local"))!;
  const options = Array.from(attach.querySelectorAll("option")).map((o) => o.textContent);
  expect(options).toContain("api refactor");
  expect(options).not.toContain("mm-2");
});

test("double-clicking the tile title renames the session", async () => {
  const layout = { shape: { rows: 1, cols: 1 }, tiles: [{ serverId: "local", sessionId: 1 }] };
  const fetchMock = mockFetch(layout);

  render(<GridPage />);
  const title = await screen.findByText("#1 · claude");

  await userEvent.dblClick(title);
  const input = await screen.findByLabelText<HTMLInputElement>("rename session 1");
  expect(input.value).toBe("");

  await userEvent.type(input, "api refactor");
  fireEvent.keyDown(input, { key: "Enter" });

  await waitFor(() => {
    const put = fetchMock.mock.calls.find(
      ([url, init]) => String(url).includes("/api/sessions/1/label") && init?.method === "PUT",
    );
    expect(put).toBeTruthy();
    expect(JSON.parse(String(put?.[1]?.body))).toEqual({ label: "api refactor" });
  });
});

test("Escape cancels a rename without writing", async () => {
  const layout = { shape: { rows: 1, cols: 1 }, tiles: [{ serverId: "local", sessionId: 1 }] };
  const fetchMock = mockFetch(layout);

  render(<GridPage />);
  const title = await screen.findByText("#1 · claude");

  await userEvent.dblClick(title);
  const input = await screen.findByLabelText<HTMLInputElement>("rename session 1");
  await userEvent.type(input, "discard me");
  fireEvent.keyDown(input, { key: "Escape" });

  expect(await screen.findByText("#1 · claude")).toBeTruthy();
  expect(fetchMock.mock.calls.some(([url, init]) => String(url).includes("/label") && init?.method === "PUT")).toBe(
    false,
  );
});

test("opening the rename input on a labelled session preselects its text", async () => {
  const layout = { shape: { rows: 1, cols: 1 }, tiles: [{ serverId: "local", sessionId: 2 }] };
  mockFetch(layout);

  render(<GridPage />);
  const title = await screen.findByText("#2 · api refactor");

  await userEvent.dblClick(title);
  const input = await screen.findByLabelText<HTMLInputElement>("rename session 2");

  expect(input.selectionStart).toBe(0);
  expect(input.selectionEnd).toBe(input.value.length);
});

test("clicking away from the rename input saves it, like Enter", async () => {
  const layout = { shape: { rows: 1, cols: 1 }, tiles: [{ serverId: "local", sessionId: 1 }] };
  const fetchMock = mockFetch(layout);

  render(<GridPage />);
  const title = await screen.findByText("#1 · claude");

  await userEvent.dblClick(title);
  const input = await screen.findByLabelText<HTMLInputElement>("rename session 1");
  await userEvent.type(input, "api refactor");
  await userEvent.click(document.body);

  await waitFor(() => {
    const put = fetchMock.mock.calls.find(
      ([url, init]) => String(url).includes("/api/sessions/1/label") && init?.method === "PUT",
    );
    expect(put).toBeTruthy();
    expect(JSON.parse(String(put?.[1]?.body))).toEqual({ label: "api refactor" });
  });
});

test("double-clicking the tile title does not maximize the tile", async () => {
  const layout = { shape: { rows: 1, cols: 2 }, tiles: [{ serverId: "local", sessionId: 1 }, null] };
  mockFetch(layout);

  const { container } = render(<GridPage />);
  const title = await screen.findByText("#1 · claude");

  await userEvent.dblClick(title);
  expect(container.querySelector(".tile-maximized")).toBeNull();
});

test("tiles are positioned from the stored row and column sizes", async () => {
  mockFetch({
    shape: { rows: 2, cols: 2 },
    tiles: [
      { serverId: "local", sessionId: 1 },
      { serverId: "local", sessionId: 2 },
      { serverId: "local", sessionId: 5 },
      null,
    ],
    rowSizes: [0.3, 0.7],
    colSizes: [
      [0.35, 0.65],
      [0.8, 0.2],
    ],
  });
  render(<GridPage />);
  // GridPage paints an empty placeholder grid (also carrying data-tile-index)
  // before the mocked layout fetch resolves, so wait for real session content
  // rather than mere attribute presence — otherwise the assertions below can
  // race the placeholder's equal-split sizing.
  await screen.findByTestId("term-1");
  const first = document.querySelector('[data-tile-index="0"]') as HTMLElement;
  expect(first.style.top).toContain("0%");
  expect(first.style.left).toContain("0%");
  expect(first.style.width).toContain("35%");
  expect(first.style.height).toContain("30%");

  const third = document.querySelector('[data-tile-index="2"]') as HTMLElement;
  expect(third.style.top).toContain("30%");
  expect(third.style.width).toContain("80%");
  expect(third.style.height).toContain("70%");
});

function gridRect(width: number, height: number) {
  // jsdom reports zero-sized boxes; the drag math divides by the container
  // size, so stub it.
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue({
    x: 0,
    y: 0,
    top: 0,
    left: 0,
    right: width,
    bottom: height,
    width,
    height,
    toJSON: () => ({}),
  } as DOMRect);
}

test("dragging a row divider persists new row heights", async () => {
  const fetchMock = mockFetch({
    shape: { rows: 2, cols: 2 },
    tiles: [
      { serverId: "local", sessionId: 1 },
      { serverId: "local", sessionId: 2 },
      { serverId: "local", sessionId: 5 },
      null,
    ],
  });
  gridRect(1000, 1000);
  render(<GridPage />);
  await screen.findByTestId("term-1");
  const divider = document.querySelector('[data-divider="row-0"]') as HTMLElement;

  fireEvent.pointerDown(divider, { pointerId: 1, clientX: 500, clientY: 500 });
  fireEvent.pointerMove(divider, { pointerId: 1, clientX: 500, clientY: 300 });
  fireEvent.pointerUp(divider, { pointerId: 1, clientX: 500, clientY: 300 });

  await waitFor(() => {
    const put = fetchMock.mock.calls.find(
      ([url, init]) => String(url).includes("/api/layout") && init?.method === "PUT",
    );
    expect(put).toBeTruthy();
    const body = JSON.parse(String(put![1]!.body));
    expect(body.rowSizes[0]).toBeCloseTo(0.3, 6);
    expect(body.rowSizes[1]).toBeCloseTo(0.7, 6);
  });
});

test("a column divider only changes its own row", async () => {
  const fetchMock = mockFetch({
    shape: { rows: 2, cols: 2 },
    tiles: [
      { serverId: "local", sessionId: 1 },
      { serverId: "local", sessionId: 2 },
      { serverId: "local", sessionId: 5 },
      null,
    ],
  });
  gridRect(1000, 1000);
  render(<GridPage />);
  await screen.findByTestId("term-1");
  const divider = document.querySelector('[data-divider="col-1-0"]') as HTMLElement;

  fireEvent.pointerDown(divider, { pointerId: 1, clientX: 500, clientY: 750 });
  fireEvent.pointerMove(divider, { pointerId: 1, clientX: 800, clientY: 750 });
  fireEvent.pointerUp(divider, { pointerId: 1, clientX: 800, clientY: 750 });

  await waitFor(() => {
    const put = fetchMock.mock.calls.find(
      ([url, init]) => String(url).includes("/api/layout") && init?.method === "PUT",
    );
    const body = JSON.parse(String(put![1]!.body));
    expect(body.colSizes[0]).toEqual([0.5, 0.5]);
    expect(body.colSizes[1][0]).toBeCloseTo(0.8, 6);
  });
});

test("a drag never falls below the minimum tile size", async () => {
  const fetchMock = mockFetch({
    shape: { rows: 2, cols: 2 },
    tiles: [
      { serverId: "local", sessionId: 1 },
      { serverId: "local", sessionId: 2 },
      { serverId: "local", sessionId: 5 },
      null,
    ],
  });
  gridRect(1000, 1000);
  render(<GridPage />);
  await screen.findByTestId("term-1");
  const divider = document.querySelector('[data-divider="row-0"]') as HTMLElement;

  fireEvent.pointerDown(divider, { pointerId: 1, clientX: 500, clientY: 500 });
  fireEvent.pointerMove(divider, { pointerId: 1, clientX: 500, clientY: 5 });
  fireEvent.pointerUp(divider, { pointerId: 1, clientX: 500, clientY: 5 });

  await waitFor(() => {
    const put = fetchMock.mock.calls.find(
      ([url, init]) => String(url).includes("/api/layout") && init?.method === "PUT",
    );
    // 120px of a 1000px container.
    expect(JSON.parse(String(put![1]!.body)).rowSizes[0]).toBeCloseTo(0.12, 6);
  });
});

test("double-clicking a divider equalizes that axis", async () => {
  const fetchMock = mockFetch({
    shape: { rows: 2, cols: 2 },
    tiles: [
      { serverId: "local", sessionId: 1 },
      { serverId: "local", sessionId: 2 },
      { serverId: "local", sessionId: 5 },
      null,
    ],
    rowSizes: [0.2, 0.8],
    colSizes: [
      [0.5, 0.5],
      [0.5, 0.5],
    ],
  });
  gridRect(1000, 1000);
  render(<GridPage />);
  await screen.findByTestId("term-1");
  const divider = document.querySelector('[data-divider="row-0"]') as HTMLElement;
  fireEvent.doubleClick(divider);

  await waitFor(() => {
    const put = fetchMock.mock.calls.find(
      ([url, init]) => String(url).includes("/api/layout") && init?.method === "PUT",
    );
    expect(JSON.parse(String(put![1]!.body)).rowSizes).toEqual([0.5, 0.5]);
  });
});

function layoutPuts(fetchMock: ReturnType<typeof mockFetch>) {
  return fetchMock.mock.calls.filter(([url, init]) => String(url).includes("/api/layout") && init?.method === "PUT");
}

test("a drag with several pointermoves persists exactly one PUT", async () => {
  const fetchMock = mockFetch({
    shape: { rows: 2, cols: 2 },
    tiles: [
      { serverId: "local", sessionId: 1 },
      { serverId: "local", sessionId: 2 },
      { serverId: "local", sessionId: 5 },
      null,
    ],
  });
  gridRect(1000, 1000);
  render(<GridPage />);
  await screen.findByTestId("term-1");
  const divider = document.querySelector('[data-divider="row-0"]') as HTMLElement;

  fireEvent.pointerDown(divider, { pointerId: 1, clientX: 500, clientY: 500 });
  fireEvent.pointerMove(divider, { pointerId: 1, clientX: 500, clientY: 450 });
  fireEvent.pointerMove(divider, { pointerId: 1, clientX: 500, clientY: 400 });
  fireEvent.pointerMove(divider, { pointerId: 1, clientX: 500, clientY: 300 });
  fireEvent.pointerUp(divider, { pointerId: 1, clientX: 500, clientY: 300 });

  await waitFor(() => expect(layoutPuts(fetchMock)).toHaveLength(1));
});

test("a double-click reset persists exactly one PUT", async () => {
  const fetchMock = mockFetch({
    shape: { rows: 2, cols: 2 },
    tiles: [
      { serverId: "local", sessionId: 1 },
      { serverId: "local", sessionId: 2 },
      { serverId: "local", sessionId: 5 },
      null,
    ],
    rowSizes: [0.2, 0.8],
  });
  gridRect(1000, 1000);
  render(<GridPage />);
  await screen.findByTestId("term-1");
  const divider = document.querySelector('[data-divider="row-0"]') as HTMLElement;
  fireEvent.doubleClick(divider);

  await waitFor(() => expect(layoutPuts(fetchMock)).toHaveLength(1));
});

test("a pointerdown/pointerup with no movement does not commit or PUT", async () => {
  const fetchMock = mockFetch({
    shape: { rows: 2, cols: 2 },
    tiles: [
      { serverId: "local", sessionId: 1 },
      { serverId: "local", sessionId: 2 },
      { serverId: "local", sessionId: 5 },
      null,
    ],
  });
  gridRect(1000, 1000);
  render(<GridPage />);
  await screen.findByTestId("term-1");
  const divider = document.querySelector('[data-divider="row-0"]') as HTMLElement;

  fireEvent.pointerDown(divider, { pointerId: 1, clientX: 500, clientY: 500 });
  fireEvent.pointerUp(divider, { pointerId: 1, clientX: 500, clientY: 500 });

  // Give any errant async work a turn before asserting nothing happened.
  await new Promise((resolve) => setTimeout(resolve, 0));
  expect(layoutPuts(fetchMock)).toHaveLength(0);
});

test("the reflow gate is released when GridDividers unmounts mid-drag", async () => {
  mockFetch({
    shape: { rows: 2, cols: 2 },
    tiles: [
      { serverId: "local", sessionId: 1 },
      { serverId: "local", sessionId: 2 },
      { serverId: "local", sessionId: 5 },
      null,
    ],
  });
  gridRect(1000, 1000);
  const media = stubMatchMedia(false);
  const { unmount } = render(<GridPage />);
  await screen.findByTestId("term-1");
  const divider = document.querySelector('[data-divider="row-0"]') as HTMLElement;

  fireEvent.pointerDown(divider, { pointerId: 1, clientX: 500, clientY: 500 });
  fireEvent.pointerMove(divider, { pointerId: 1, clientX: 500, clientY: 300 });
  expect(isReflowHeld()).toBe(true);

  // Crossing the mobile breakpoint unmounts the desktop grid branch (and
  // GridDividers with it) without ever delivering a pointerup/pointercancel.
  media.setMatches(true);
  await screen.findByText("1/3");
  expect(isReflowHeld()).toBe(false);

  unmount();
});

test("clicking a dir button solos that directory's tiles and quick-add buttons", async () => {
  mockFetch({ shape: { rows: 1, cols: 2 }, tiles: [{ serverId: "local", sessionId: 1 }, null] });
  render(<GridPage />);
  // Session 1 (/a) is tiled; sessions 2 (/b) and 5 (/d) are unplaced.
  await screen.findByTestId("term-1");
  expect(screen.getByRole("button", { name: /add to grid — \/b/ })).toBeInTheDocument();

  // Solo /a: its tile stays, every other directory's quick-add goes.
  await userEvent.click(screen.getByRole("button", { name: /show only sessions in \/a/ }));
  await waitFor(() => expect(screen.queryByRole("button", { name: /add to grid — \/b/ })).not.toBeInTheDocument());
  expect(screen.queryByRole("button", { name: /add to grid — \/d/ })).not.toBeInTheDocument();
  expect(screen.getByTestId("term-1")).toBeInTheDocument();
  expect(soloDir()).toBe("/a");

  // Clicking a different button moves the solo rather than adding to it.
  await userEvent.click(screen.getByRole("button", { name: /show only sessions in \/b/ }));
  await waitFor(() => expect(screen.getByTestId("term-1")).not.toBeVisible());
  expect(screen.getByRole("button", { name: /add to grid — \/b/ })).toBeInTheDocument();
  expect(soloDir()).toBe("/b");

  // Clicking the soloed button goes back to showing everything.
  await userEvent.click(screen.getByRole("button", { name: /show all directories/ }));
  await screen.findByTestId("term-1");
  expect(screen.getByRole("button", { name: /add to grid — \/d/ })).toBeInTheDocument();
  expect(soloDir()).toBeNull();
});

test("Ctrl+Alt+arrows rotate the solo and Ctrl+Alt+0 clears it", async () => {
  mockFetch({ shape: { rows: 1, cols: 2 }, tiles: [{ serverId: "local", sessionId: 1 }, null] });
  render(<GridPage />);
  await screen.findByTestId("term-1");
  // Running directories, in bar order: /a, /b, /d. /c has only a dead session.
  const press = (key: string, code?: string) =>
    fireEvent.keyDown(window, { key, code: code ?? key, ctrlKey: true, altKey: true });

  press("ArrowRight");
  await waitFor(() => expect(soloDir()).toBe("/a"));
  press("ArrowRight");
  await waitFor(() => expect(soloDir()).toBe("/b"));
  // Backwards retraces the ring.
  press("ArrowLeft");
  await waitFor(() => expect(soloDir()).toBe("/a"));
  // Past the first button is show-all, then round to the last one.
  press("ArrowLeft");
  await waitFor(() => expect(soloDir()).toBeNull());
  press("ArrowLeft");
  await waitFor(() => expect(soloDir()).toBe("/d"));

  // Ctrl+Alt+0 clears from anywhere; macOS reports `key` as "º" with Alt held.
  press("º", "Digit0");
  await waitFor(() => expect(soloDir()).toBeNull());
  await screen.findByTestId("term-1");
});

test("the solo shortcut ignores other modifier combinations", async () => {
  mockFetch({ shape: { rows: 1, cols: 2 }, tiles: [{ serverId: "local", sessionId: 1 }, null] });
  render(<GridPage />);
  await screen.findByTestId("term-1");
  for (const mods of [{ ctrlKey: true }, { altKey: true }, { ctrlKey: true, altKey: true, shiftKey: true }]) {
    fireEvent.keyDown(window, { key: "ArrowRight", code: "ArrowRight", ...mods });
  }
  expect(soloDir()).toBeNull();
});

test("a solo shows its ended sessions and hides tiles with no known session", async () => {
  mockFetch(
    {
      shape: { rows: 2, cols: 2 },
      tiles: [
        { serverId: "local", sessionId: 1 },
        { serverId: "local", sessionId: 4 },
        { serverId: "local", sessionId: 99 },
        null,
      ],
    },
    // Session 4 is dead, and in the same directory as running session 1.
    sessions.map((s) => (s.id === 4 ? { ...s, dir: "/a" } : s)),
  );
  render(<GridPage />);
  await screen.findByTestId("term-1");
  expect(screen.getByText("session ended")).toBeInTheDocument();
  expect(screen.getByTestId("term-99")).toBeInTheDocument();

  await userEvent.click(screen.getByRole("button", { name: /show only sessions in \/a/ }));

  // A tile whose session is unknown has no directory to match, so it hides…
  await waitFor(() => expect(screen.getByTestId("term-99")).not.toBeVisible());
  // …while visibility follows the directory whatever the status, so the ended
  // session in the soloed directory keeps its dismiss button.
  expect(screen.getByTestId("term-1")).toBeInTheDocument();
  expect(screen.getByText("session ended")).toBeInTheDocument();
});

test("the solo is read back from storage on mount", async () => {
  localStorage.setItem("multimux.soloDir", '"/b"');
  mockFetch({ shape: { rows: 1, cols: 2 }, tiles: [{ serverId: "local", sessionId: 1 }, null] });
  render(<GridPage />);
  await screen.findByRole("button", { name: /show all directories/ });
  expect(screen.getByTestId("term-1")).not.toBeVisible();
});

test("removing a tile while filtered removes the right session", async () => {
  const fetchMock = mockFetch({
    shape: { rows: 1, cols: 2 },
    tiles: [
      { serverId: "local", sessionId: 1 },
      { serverId: "local", sessionId: 2 },
    ],
  });
  render(<GridPage />);
  await screen.findByTestId("term-2");
  // Solo /b, so session 2 is the only tile on screen and sits at view index 0.
  await userEvent.click(screen.getByRole("button", { name: /show only sessions in \/b/ }));
  await waitFor(() => expect(screen.getByTestId("term-1")).not.toBeVisible());

  await userEvent.click(screen.getByRole("button", { name: /remove session 2 from grid/ }));
  await waitFor(() => {
    const puts = fetchMock.mock.calls.filter(
      ([url, init]) => String(url).includes("/api/layout") && init?.method === "PUT",
    );
    // The persisted layout keeps hidden session 1 and drops only session 2.
    expect(JSON.parse(puts[puts.length - 1][1]?.body as string).tiles).toEqual([{ serverId: "local", sessionId: 1 }]);
  });
});

test("drag-swapping tiles while soloed reorders the overlay, not the stored layout", async () => {
  const fetchMock = mockFetch(
    {
      shape: { rows: 2, cols: 2 },
      tiles: [
        { serverId: "local", sessionId: 1 },
        { serverId: "local", sessionId: 2 },
        { serverId: "local", sessionId: 5 },
        null,
      ],
    },
    // Sessions 2 and 5 share a directory, so one solo leaves two tiles.
    sessions.map((s) => (s.id === 5 ? { ...s, dir: "/b" } : s)),
  );
  render(<GridPage />);
  await screen.findByTestId("term-5");
  await userEvent.click(screen.getByRole("button", { name: /show only sessions in \/b/ }));
  await waitFor(() => expect(screen.getByTestId("term-1")).not.toBeVisible());
  const putsBefore = fetchMock.mock.calls.filter(
    ([url, init]) => String(url).includes("/api/layout") && init?.method === "PUT",
  ).length;

  const tiles = document.querySelectorAll(".tile");
  const dt = makeDataTransfer();
  fireEvent.dragStart(tiles[0], { dataTransfer: dt });
  fireEvent.drop(tiles[1], { dataTransfer: dt });

  // On screen the two visible tiles traded places…
  await waitFor(() => {
    const after = document.querySelectorAll(".tile");
    expect(after[0].querySelector("[data-testid=term-5]")).not.toBeNull();
    expect(after[1].querySelector("[data-testid=term-2]")).not.toBeNull();
  });
  // …and only /b's overlay records it.
  expect(JSON.parse(localStorage.getItem("multimux.viewLayout")!)["/b"].order).toEqual(["local:5", "local:2"]);
  expect(
    fetchMock.mock.calls.filter(([url, init]) => String(url).includes("/api/layout") && init?.method === "PUT").length,
  ).toBe(putsBefore);
});

test("a stored solo with no button shows everything and does not disable persistence", async () => {
  // The last session in the soloed directory can end from anywhere (tmux exit,
  // another tab, the CLI). The stored solo is never pruned, but a solo with no
  // button is not in effect — so nothing is filtered out unseeably, and
  // splitter drags still reach the daemon.
  localStorage.setItem("multimux.soloDir", '"/gone"');
  const fetchMock = mockFetch({
    shape: { rows: 2, cols: 2 },
    tiles: [
      { serverId: "local", sessionId: 1 },
      { serverId: "local", sessionId: 2 },
      { serverId: "local", sessionId: 5 },
      null,
    ],
  });
  gridRect(1000, 1000);
  render(<GridPage />);
  await screen.findByTestId("term-1");

  // No button for the stale path, and every tile is on screen.
  expect(screen.queryByRole("button", { name: /sessions in \/gone/ })).not.toBeInTheDocument();
  expect(screen.getByTestId("term-2")).toBeInTheDocument();
  expect(screen.getByTestId("term-5")).toBeInTheDocument();

  const divider = document.querySelector('[data-divider="row-0"]') as HTMLElement;
  fireEvent.pointerDown(divider, { pointerId: 1, clientX: 500, clientY: 500 });
  fireEvent.pointerMove(divider, { pointerId: 1, clientX: 500, clientY: 300 });
  fireEvent.pointerUp(divider, { pointerId: 1, clientX: 500, clientY: 300 });

  await waitFor(() => {
    const puts = fetchMock.mock.calls.filter(
      ([url, init]) => String(url).includes("/api/layout") && init?.method === "PUT",
    );
    expect(puts.length).toBeGreaterThan(0);
    expect(JSON.parse(puts[puts.length - 1][1]?.body as string).rowSizes[0]).toBeCloseTo(0.3, 6);
  });

  // The stale value is left in storage, ready for its directory to come back.
  expect(soloDir()).toBe("/gone");
});

test("attaching a session outside the solo from an empty tile's dropdown clears the solo", async () => {
  mockFetch({ shape: { rows: 1, cols: 2 }, tiles: [{ serverId: "local", sessionId: 1 }, null] });
  render(<GridPage />);
  await screen.findByTestId("term-1");

  // Solo /a — session 2 (/b) stays offered in the empty tile's attach dropdown
  // (EmptyTile does not filter by directory), but attaching it must not land a
  // tile that the filter immediately hides again.
  await userEvent.click(screen.getByRole("button", { name: /show only sessions in \/a/ }));
  await waitFor(() => expect(screen.queryByRole("button", { name: /add to grid — \/d/ })).not.toBeInTheDocument());

  const attach = screen.getAllByRole("combobox").find((b) => b.textContent?.includes("attach session on local"))!;
  await userEvent.selectOptions(attach, "2");

  await screen.findByTestId("term-2");
  // The solo is cleared, not moved: session 1 is still on screen.
  expect(screen.getByTestId("term-1")).toBeInTheDocument();
  expect(soloDir()).toBeNull();
});

test("splitter drags while filtered resize the view without persisting", async () => {
  const fetchMock = mockFetch(
    {
      shape: { rows: 2, cols: 2 },
      tiles: [
        { serverId: "local", sessionId: 1 },
        { serverId: "local", sessionId: 2 },
        { serverId: "local", sessionId: 5 },
        null,
      ],
    },
    sessions.map((s) => (s.id === 5 ? { ...s, dir: "/b" } : s)),
  );
  const { container } = render(<GridPage />);
  await screen.findByTestId("term-5");
  await userEvent.click(screen.getByRole("button", { name: /show only sessions in \/b/ }));
  await waitFor(() => expect(screen.getByTestId("term-1")).not.toBeVisible());

  const putsBefore = fetchMock.mock.calls.filter(
    ([url, init]) => String(url).includes("/api/layout") && init?.method === "PUT",
  ).length;

  const divider = container.querySelector('[data-divider="col-0-0"]') as HTMLElement;
  vi.spyOn(container.querySelector(".grid") as HTMLElement, "getBoundingClientRect").mockReturnValue({
    width: 1000,
    height: 800,
    top: 0,
    left: 0,
    right: 1000,
    bottom: 800,
    x: 0,
    y: 0,
    toJSON: () => ({}),
  } as DOMRect);
  divider.setPointerCapture = () => {};
  fireEvent.pointerDown(divider, { button: 0, clientX: 500, clientY: 10 });
  fireEvent.pointerMove(divider, { clientX: 700, clientY: 10 });
  fireEvent.pointerUp(divider, { clientX: 700, clientY: 10 });

  // The view resized...
  await waitFor(() => expect(container.querySelector('[data-divider="col-0-0"]')).toHaveStyle({ left: "70%" }));
  // ...and nothing was written to the daemon.
  const putsAfter = fetchMock.mock.calls.filter(
    ([url, init]) => String(url).includes("/api/layout") && init?.method === "PUT",
  ).length;
  expect(putsAfter).toBe(putsBefore);
  expect(isReflowHeld()).toBe(false);
});

test("clearing the filter restores the stored sizes", async () => {
  mockFetch({
    shape: { rows: 1, cols: 2 },
    tiles: [
      { serverId: "local", sessionId: 1 },
      { serverId: "local", sessionId: 2 },
    ],
    rowSizes: [1],
    colSizes: [[0.3, 0.7]],
  });
  const { container } = render(<GridPage />);
  await screen.findByTestId("term-2");
  await userEvent.click(screen.getByRole("button", { name: /show only sessions in \/b/ }));
  await waitFor(() => expect(screen.getByTestId("term-1")).not.toBeVisible());
  await userEvent.click(screen.getByRole("button", { name: /show all directories/ }));
  await screen.findByTestId("term-1");
  expect(container.querySelector('[data-divider="col-0-0"]')).toHaveStyle({ left: "30%" });
});

// In all mode the launcher follows the session the user is working in, so
// "+ New" opens a second session alongside the first without re-picking the
// directory. The focused session's dir is a launch dir plus a subdir; both
// halves land in the launcher.
test("focusing a tile aims the launcher at that session's directory and subdir", async () => {
  const layout = {
    shape: { rows: 1, cols: 2 },
    tiles: [
      { serverId: "local", sessionId: 1 },
      { serverId: "local", sessionId: 2 },
    ],
  };
  const fetchMock = mockFetch(layout, [
    { id: 1, tmuxName: "mm-1", toolId: 1, dir: "/Users/jon/Repos/multimux", status: "running" },
    { id: 2, tmuxName: "mm-2", toolId: 1, dir: "/Users/jon/Repos/other/pkg", status: "running" },
  ]);
  const { container } = render(<GridPage />);

  const dirSelect = await screen.findByLabelText<HTMLSelectElement>("dir");
  const subdir = screen.getByLabelText<HTMLInputElement>("subdirectory");
  await screen.findByTestId("term-2");

  fireEvent.focusIn(container.querySelector('[data-tile-index="1"]')!);
  await waitFor(() => expect(dirSelect.value).toBe("2"));
  expect(subdir.value).toBe("pkg");

  fireEvent.focusIn(container.querySelector('[data-tile-index="0"]')!);
  await waitFor(() => expect(dirSelect.value).toBe("1"));
  expect(subdir.value).toBe("");

  await userEvent.click(screen.getByText("+ New"));
  const post = fetchMock.mock.calls.find(([, init]) => init?.method === "POST");
  expect(JSON.parse(String(post?.[1]?.body))).toEqual({ toolId: 1, dirId: 1, subdir: "" });
});

// A solo is the standing answer to "which directory am I in", so it outranks
// whatever tile happens to hold focus.
test("a soloed directory outranks the focused tile", async () => {
  const layout = {
    shape: { rows: 1, cols: 2 },
    tiles: [
      { serverId: "local", sessionId: 1 },
      { serverId: "local", sessionId: 2 },
    ],
  };
  mockFetch(layout, [
    { id: 1, tmuxName: "mm-1", toolId: 1, dir: "/Users/jon/Repos/multimux", status: "running" },
    { id: 2, tmuxName: "mm-2", toolId: 1, dir: "/Users/jon/Repos/other/pkg", status: "running" },
  ]);
  const { container } = render(<GridPage />);

  const dirSelect = await screen.findByLabelText<HTMLSelectElement>("dir");
  await screen.findByTestId("term-2");
  fireEvent.focusIn(container.querySelector('[data-tile-index="1"]')!);
  await waitFor(() => expect(dirSelect.value).toBe("2"));

  await userEvent.click(screen.getByRole("button", { name: /^multimux 1/ }));
  await waitFor(() => expect(dirSelect.value).toBe("1"));
  expect(screen.getByLabelText<HTMLInputElement>("subdirectory").value).toBe("");
});

test("a soloed directory keeps its columns and order across a switch away and back", async () => {
  const fetchMock = mockFetch(
    {
      shape: { rows: 2, cols: 2 },
      tiles: [
        { serverId: "local", sessionId: 1 },
        { serverId: "local", sessionId: 2 },
        { serverId: "local", sessionId: 5 },
        null,
      ],
    },
    sessions.map((s) => (s.id === 5 ? { ...s, dir: "/b" } : s)),
  );
  render(<GridPage />);
  await screen.findByTestId("term-5");
  await userEvent.click(screen.getByRole("button", { name: /show only sessions in \/b/ }));
  await waitFor(() => expect(screen.getByTestId("term-1")).not.toBeVisible());

  // One column, and the two tiles swapped.
  await userEvent.click(screen.getByLabelText("fewer columns"));
  const tiles = document.querySelectorAll(".tile");
  const dt = makeDataTransfer();
  fireEvent.dragStart(tiles[0], { dataTransfer: dt });
  fireEvent.drop(tiles[1], { dataTransfer: dt });
  await waitFor(() => {
    const after = document.querySelectorAll(".tile");
    expect(after[0].querySelector("[data-testid=term-5]")).not.toBeNull();
  });

  // Out to /a, whose own view is untouched by any of that…
  await userEvent.click(screen.getByRole("button", { name: /show only sessions in \/a/ }));
  await screen.findByTestId("term-1");
  // …and back to /b, which is exactly as it was left.
  await userEvent.click(screen.getByRole("button", { name: /show only sessions in \/b/ }));
  await waitFor(() => {
    const after = document.querySelectorAll(".tile");
    expect(after[0].querySelector("[data-testid=term-5]")).not.toBeNull();
    expect(after[1].querySelector("[data-testid=term-2]")).not.toBeNull();
  });
  expect(screen.getByLabelText("fewer columns")).toBeDisabled();

  // The stored layout never saw any of it.
  const puts = fetchMock.mock.calls.filter(
    ([url, init]) => String(url).includes("/api/layout") && init?.method === "PUT",
  );
  expect(puts).toHaveLength(0);
});

test("a stored overlay is applied on mount and the unfiltered grid ignores it", async () => {
  localStorage.setItem("multimux.soloDir", '"/b"');
  localStorage.setItem(
    "multimux.viewLayout",
    JSON.stringify({ "/b": { cols: 1, order: ["local:5", "local:2"], rowSizes: [], colSizes: [] } }),
  );
  mockFetch(
    {
      shape: { rows: 2, cols: 2 },
      tiles: [
        { serverId: "local", sessionId: 1 },
        { serverId: "local", sessionId: 2 },
        { serverId: "local", sessionId: 5 },
        null,
      ],
    },
    sessions.map((s) => (s.id === 5 ? { ...s, dir: "/b" } : s)),
  );
  render(<GridPage />);
  await screen.findByTestId("term-5");

  await waitFor(() => {
    const tiles = document.querySelectorAll(".tile");
    expect(tiles[0].querySelector("[data-testid=term-5]")).not.toBeNull();
    expect(tiles[1].querySelector("[data-testid=term-2]")).not.toBeNull();
  });

  // Clearing the solo restores the stored layout's own order and columns.
  await userEvent.click(screen.getByRole("button", { name: /show all directories/ }));
  await screen.findByTestId("term-1");
  await waitFor(() => {
    const tiles = document.querySelectorAll(".tile");
    expect(tiles[0].querySelector("[data-testid=term-1]")).not.toBeNull();
    expect(tiles[1].querySelector("[data-testid=term-2]")).not.toBeNull();
    expect(tiles[2].querySelector("[data-testid=term-5]")).not.toBeNull();
  });
});

test("a session launched into a soloed directory lands at the end of its view", async () => {
  // mockFetch's session POST returns session 3 in /a, so solo /a first; the
  // session list carries it too, or the new tile would filter straight out.
  mockFetch({ shape: { rows: 1, cols: 2 }, tiles: [{ serverId: "local", sessionId: 1 }, null] }, [
    ...sessions,
    { id: 3, tmuxName: "mm-3", toolId: 1, dir: "/a", status: "running" },
  ]);
  render(<GridPage />);
  await screen.findByTestId("term-1");
  await userEvent.click(screen.getByRole("button", { name: /show only sessions in \/a/ }));
  // Give /a an overlay by dropping to one column, so the append rule is
  // exercised against a real overlay rather than the no-overlay path.
  await userEvent.click(screen.getByLabelText("fewer columns"));

  // "+ New" is the launch button itself — one click.
  await userEvent.click(screen.getByText("+ New"));

  await waitFor(() => {
    const tiles = document.querySelectorAll(".tile");
    expect(tiles[0].querySelector("[data-testid=term-1]")).not.toBeNull();
    expect(tiles[1].querySelector("[data-testid=term-3]")).not.toBeNull();
  });
});

test("a failed session refresh keeps the dir buttons and the solo", async () => {
  // The pills are derived from the session lists, so a refresh that fails —
  // a sleeping PWA, a phone changing networks — must not be mistaken for
  // "this daemon has no sessions". Losing them silently drops the solo too.
  const layout = { shape: { rows: 1, cols: 2 }, tiles: [{ serverId: "local", sessionId: 1 }, null] };
  const fetchMock = mockFetch(layout);
  render(<GridPage />);
  await screen.findByTestId("term-1");
  await userEvent.click(screen.getByRole("button", { name: /show only sessions in \/a/ }));

  const reachable = fetchMock.getMockImplementation()!;
  fetchMock.mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
    if (String(input).includes("/api/sessions")) throw new TypeError("Failed to fetch");
    return reachable(input, init);
  });

  const calls = vi.mocked(useEvents).mock.calls;
  const onEvent = calls[calls.length - 1][1];
  await act(async () => onEvent("git_changed"));

  expect(screen.getByRole("button", { name: /show all directories/ })).toBeInTheDocument();
  expect(screen.getByRole("button", { name: /show only sessions in \/b/ })).toBeInTheDocument();
  expect(soloDir()).toBe("/a");
});

test("returning to the tab resyncs sessions and layout", async () => {
  // The events socket is the only thing that refetches, so a tab that missed
  // events while it was hidden (or whose socket died there) would otherwise
  // render stale sessions — no dir buttons, no quick-adds — until a reload.
  const layout = { shape: { rows: 1, cols: 1 }, tiles: [{ serverId: "local", sessionId: 1 }] };
  const fetchMock = mockFetch(layout);
  render(<GridPage />);
  await screen.findByTestId("term-1");

  const gets = (path: string) =>
    fetchMock.mock.calls.filter(([input, init]) => String(input).includes(path) && (init?.method ?? "GET") === "GET")
      .length;
  const sessionsBefore = gets("/api/sessions");
  const layoutBefore = gets("/api/layout");

  act(() => {
    document.dispatchEvent(new Event("visibilitychange"));
  });

  await waitFor(() => expect(gets("/api/sessions")).toBeGreaterThan(sessionsBefore));
  await waitFor(() => expect(gets("/api/layout")).toBeGreaterThan(layoutBefore));
});

test("a filtered-out tile stays mounted so switching back does not rebuild it", async () => {
  // Unmounting a tile disposes xterm and kills the session's `tmux
  // attach-session`, so every solo switch would respawn and redraw whatever it
  // lands on. The node must survive, out of view.
  mockFetch({
    shape: { rows: 1, cols: 2 },
    tiles: [
      { serverId: "local", sessionId: 1 },
      { serverId: "local", sessionId: 2 },
    ],
  });
  render(<GridPage />);
  const b = await screen.findByTestId("term-2"); // session 2 lives in /b

  await userEvent.click(screen.getByRole("button", { name: /show only sessions in \/a/ }));
  await waitFor(() => expect(screen.getByTestId("term-2")).not.toBeVisible());
  expect(screen.getByTestId("term-2")).toBe(b);
  expect(screen.getByTestId("term-1")).toBeVisible();

  await userEvent.click(screen.getByRole("button", { name: /show only sessions in \/b/ }));
  await waitFor(() => expect(screen.getByTestId("term-2")).toBeVisible());
  expect(screen.getByTestId("term-2")).toBe(b);
  expect(screen.getByTestId("term-1")).not.toBeVisible();
});
