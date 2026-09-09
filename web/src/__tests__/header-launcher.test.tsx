import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { vi } from "vitest";
import HeaderLauncher from "../grid/HeaderLauncher";
import type { Server } from "../servers";

const servers: Server[] = [
  { id: "local", origin: "http://local.test", name: "local" },
  { id: "r1", origin: "http://remote.test", name: "box-a" },
];
const localTools = [{ id: 1, name: "claude", command: "claude" }];
const localDirs = [{ id: 7, name: "multimux", path: "/repos/multimux" }];
const twoDirs = [
  { id: 7, name: "multimux", path: "/repos/multimux" },
  { id: 8, name: "home", path: "/home/jon" },
];

afterEach(() => {
  vi.restoreAllMocks();
});

/**
 * A daemon with one root, an optional per-root launch history and an optional
 * filesystem keyed by the subdir the picker asks about. The `/subdirs` check
 * must come before the `/api/dirs` one — the history path contains it.
 */
function mockDaemon({
  dirs = localDirs,
  history = {},
  children = {},
  post,
}: {
  dirs?: { id: number; name: string; path: string }[];
  history?: Record<number, string[]>;
  children?: Record<string, string[]>;
  post?: () => Response;
} = {}) {
  return vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const url = String(input);
    const kids = url.match(/\/api\/dirs\/\d+\/children\?path=([^&]*)/);
    if (kids) return new Response(JSON.stringify(children[decodeURIComponent(kids[1])] ?? []));
    const hist = url.match(/\/api\/dirs\/(\d+)\/subdirs/);
    if (hist) {
      if ((init?.method ?? "GET") === "DELETE") return new Response(null, { status: 204 });
      return new Response(JSON.stringify(history[Number(hist[1])] ?? []));
    }
    if (url.includes("/api/tools")) return new Response(JSON.stringify(localTools));
    if (url.includes("/api/dirs")) return new Response(JSON.stringify(dirs));
    if (url.includes("/api/sessions") && (init?.method ?? "GET") === "POST") {
      if (post) return post();
      return new Response(
        JSON.stringify([{ id: 3, tmuxName: "mm-3", toolId: 1, dir: "/repos/multimux", status: "running" }]),
        { status: 201 },
      );
    }
    return new Response("[]");
  });
}

async function openPicker() {
  fireEvent.click(await screen.findByText("+ New"));
  return screen.findByRole("dialog", { name: "Choose a directory" });
}

function bodyOf(fetchMock: ReturnType<typeof mockDaemon>) {
  const post = fetchMock.mock.calls.find(([, init]) => init?.method === "POST");
  return post && (JSON.parse(String(post[1]?.body)) as { toolId: number; dirId: number; subdir: string });
}

test("the header offers no directory field — location is chosen in the picker", async () => {
  mockDaemon();
  render(<HeaderLauncher servers={[servers[0]]} onLaunched={vi.fn()} />);

  await screen.findByLabelText("tool");
  expect(screen.queryByLabelText("subdirectory")).toBeNull();
  expect(screen.queryByLabelText("dir")).toBeNull();
  expect(screen.queryByRole("dialog")).toBeNull();
});

test("a configured root launches at its own path", async () => {
  const fetchMock = mockDaemon();
  render(<HeaderLauncher servers={[servers[0]]} onLaunched={vi.fn()} />);

  await openPicker();
  fireEvent.click(screen.getByText("multimux"));

  await waitFor(() => expect(bodyOf(fetchMock)).toEqual({ toolId: 1, dirId: 7, subdir: "" }));
});

// The rule the whole surface rests on: the name launches, the chevron descends.
test("the chevron drills without launching and the name below launches with the subdir", async () => {
  const fetchMock = mockDaemon({ children: { "": ["web", "internal", ".git"], web: ["src"] } });
  render(<HeaderLauncher servers={[servers[0]]} onLaunched={vi.fn()} />);

  await openPicker();
  fireEvent.click(screen.getByLabelText("open multimux"));

  expect(await screen.findByText("web")).toBeInTheDocument();
  expect(screen.getByText("internal")).toBeInTheDocument();
  // Hidden directories stay out of the way until the filter reaches for one.
  expect(screen.queryByText(".git")).toBeNull();
  expect(fetchMock.mock.calls.some(([, init]) => init?.method === "POST")).toBe(false);

  fireEvent.click(screen.getByLabelText("open web"));
  fireEvent.click(await screen.findByText("src"));

  await waitFor(() => expect(bodyOf(fetchMock)).toEqual({ toolId: 1, dirId: 7, subdir: "web/src" }));
});

test("Launch here launches the directory drilled into", async () => {
  const fetchMock = mockDaemon({ children: { "": ["web"] } });
  render(<HeaderLauncher servers={[servers[0]]} onLaunched={vi.fn()} />);

  await openPicker();
  fireEvent.click(screen.getByLabelText("open multimux"));
  fireEvent.click(await screen.findByLabelText("open web"));
  fireEvent.click(await screen.findByRole("button", { name: "Launch here" }));

  await waitFor(() => expect(bodyOf(fetchMock)).toEqual({ toolId: 1, dirId: 7, subdir: "web" }));
});

test("breadcrumbs walk back up, and Places returns to the roots", async () => {
  mockDaemon({ children: { "": ["web"], web: ["src"] } });
  render(<HeaderLauncher servers={[servers[0]]} onLaunched={vi.fn()} />);

  await openPicker();
  fireEvent.click(screen.getByLabelText("open multimux"));
  fireEvent.click(await screen.findByLabelText("open web"));
  await screen.findByText("src");

  // Crumb per segment: the root's name, then the subdir's parts.
  fireEvent.click(screen.getByRole("button", { name: "multimux" }));
  expect(await screen.findByText("web")).toBeInTheDocument();
  expect(screen.queryByText("src")).toBeNull();

  fireEvent.click(screen.getByRole("button", { name: "Places" }));
  expect(await screen.findByRole("heading", { name: "Places" })).toBeInTheDocument();
  expect(screen.queryByText("web")).toBeNull();
});

test("the filter narrows the listing and reveals dotfiles once it starts with a dot", async () => {
  mockDaemon({ children: { "": ["web", "internal", ".git"] } });
  render(<HeaderLauncher servers={[servers[0]]} onLaunched={vi.fn()} />);

  await openPicker();
  fireEvent.click(screen.getByLabelText("open multimux"));
  await screen.findByText("web");

  const filter = screen.getByLabelText("filter directories");
  fireEvent.change(filter, { target: { value: "int" } });
  expect(screen.getByText("internal")).toBeInTheDocument();
  expect(screen.queryByText("web")).toBeNull();

  fireEvent.change(filter, { target: { value: "." } });
  expect(await screen.findByText(".git")).toBeInTheDocument();

  fireEvent.change(filter, { target: { value: "zzz" } });
  expect(screen.getByText("nothing here")).toBeInTheDocument();
});

// Recents span roots: they are the whole point of keeping the configured list
// short, so they cannot be per-root the way the old subdir field was.
test("recents from every root are listed above the roots and launch in one click", async () => {
  const fetchMock = mockDaemon({ dirs: twoDirs, history: { 7: ["web/src"], 8: ["Downloads"] } });
  render(<HeaderLauncher servers={[servers[0]]} onLaunched={vi.fn()} />);

  await openPicker();
  expect(await screen.findByText("/repos/multimux/web/src")).toBeInTheDocument();
  expect(screen.getByText("/home/jon/Downloads")).toBeInTheDocument();

  fireEvent.click(screen.getByText("/home/jon/Downloads"));
  await waitFor(() => expect(bodyOf(fetchMock)).toEqual({ toolId: 1, dirId: 8, subdir: "Downloads" }));
});

test("the x forgets a recent, and only recents offer it", async () => {
  const fetchMock = mockDaemon({ history: { 7: ["web/src", "cmd"] } });
  render(<HeaderLauncher servers={[servers[0]]} onLaunched={vi.fn()} />);

  await openPicker();
  await screen.findByText("/repos/multimux/web/src");
  expect(screen.queryByLabelText("forget multimux")).toBeNull();

  fireEvent.click(screen.getByLabelText("forget /repos/multimux/web/src"));
  await waitFor(() => expect(screen.queryByText("/repos/multimux/web/src")).toBeNull());
  expect(screen.getByText("/repos/multimux/cmd")).toBeInTheDocument();
  const sent = fetchMock.mock.calls.find(([, init]) => init?.method === "DELETE");
  expect(String(sent?.[0])).toContain(`/api/dirs/7/subdirs?subdir=${encodeURIComponent("web/src")}`);
});

// A failed delete must put the entry back rather than lie about forgetting it.
test("a failed forget restores the entry and reports the error", async () => {
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const url = String(input);
    if (url.includes("/children")) return new Response("[]");
    if (/\/api\/dirs\/\d+\/subdirs/.test(url)) {
      if ((init?.method ?? "GET") === "DELETE") return new Response("nope", { status: 500 });
      return new Response(JSON.stringify(["web/src"]));
    }
    if (url.includes("/api/tools")) return new Response(JSON.stringify(localTools));
    if (url.includes("/api/dirs")) return new Response(JSON.stringify(localDirs));
    return new Response("[]");
  });
  render(<HeaderLauncher servers={[servers[0]]} onLaunched={vi.fn()} />);

  await openPicker();
  fireEvent.click(await screen.findByLabelText("forget /repos/multimux/web/src"));

  expect(await screen.findByText(/couldn't forget/i)).toBeInTheDocument();
  expect(screen.getByText("/repos/multimux/web/src")).toBeInTheDocument();
});

// The just-launched directory is the most likely next one, so it goes to the
// front of the recents without waiting for a refetch.
test("a successful launch adds its subdir to the recents", async () => {
  mockDaemon({ history: { 7: ["cmd"] }, children: { "": ["web"] } });
  render(<HeaderLauncher servers={[servers[0]]} onLaunched={vi.fn()} />);

  await openPicker();
  fireEvent.click(screen.getByLabelText("open multimux"));
  fireEvent.click(await screen.findByText("web"));

  await openPicker();
  const rows = await screen.findAllByText(/^\/repos\/multimux\/(web|cmd)$/);
  expect(rows.map((row) => row.textContent)).toEqual(["/repos/multimux/web", "/repos/multimux/cmd"]);
});

test("arrow keys walk and drill, and Enter launches the highlighted row", async () => {
  const fetchMock = mockDaemon({ children: { "": ["web", "internal"], web: ["src"] } });
  render(<HeaderLauncher servers={[servers[0]]} onLaunched={vi.fn()} />);

  const picker = await openPicker();
  fireEvent.keyDown(picker, { key: "ArrowDown" });
  fireEvent.keyDown(picker, { key: "ArrowRight" });
  await screen.findByText("web");

  fireEvent.keyDown(picker, { key: "ArrowDown" });
  fireEvent.keyDown(picker, { key: "ArrowDown" });
  fireEvent.keyDown(picker, { key: "Enter" });

  await waitFor(() => expect(bodyOf(fetchMock)).toEqual({ toolId: 1, dirId: 7, subdir: "internal" }));
});

test("Left goes back up, and Enter with nothing highlighted launches where you stand", async () => {
  const fetchMock = mockDaemon({ children: { "": ["web"], web: ["src"] } });
  render(<HeaderLauncher servers={[servers[0]]} onLaunched={vi.fn()} />);

  const picker = await openPicker();
  fireEvent.click(screen.getByLabelText("open multimux"));
  fireEvent.click(await screen.findByLabelText("open web"));
  await screen.findByText("src");

  fireEvent.keyDown(picker, { key: "ArrowLeft" });
  expect(await screen.findByText("web")).toBeInTheDocument();
  fireEvent.keyDown(picker, { key: "Enter" });

  await waitFor(() => expect(bodyOf(fetchMock)).toEqual({ toolId: 1, dirId: 7, subdir: "" }));
});

test("Escape closes the picker without launching", async () => {
  const fetchMock = mockDaemon();
  render(<HeaderLauncher servers={[servers[0]]} onLaunched={vi.fn()} />);

  const picker = await openPicker();
  fireEvent.keyDown(picker, { key: "Escape" });

  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(fetchMock.mock.calls.some(([, init]) => init?.method === "POST")).toBe(false);
});

// The picker opens where the tile that asked for a sibling already is, so the
// common case — another session in the same repo — is one click.
test("a target directory opens the picker inside it", async () => {
  const fetchMock = mockDaemon({ children: { web: ["src"] } });
  render(<HeaderLauncher servers={[servers[0]]} targetDir="/repos/multimux/web" onLaunched={vi.fn()} />);

  await openPicker();
  expect(await screen.findByText("src")).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Launch here" }));

  await waitFor(() => expect(bodyOf(fetchMock)).toEqual({ toolId: 1, dirId: 7, subdir: "web" }));
});

test("switching servers clears the previous daemon's tools and dirs until the new fetch resolves", async () => {
  // The remote daemon's tools/dirs stay pending so the switch can be observed
  // mid-flight.
  const pending: Array<() => void> = [];
  const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const url = String(input);
    if (url.includes("/children")) return new Response("[]");
    if (url.startsWith("http://remote.test")) {
      await new Promise<void>((resolve) => pending.push(resolve));
      return new Response("[]");
    }
    if (url.includes("/subdirs")) return new Response("[]");
    if (url.includes("/api/tools")) return new Response(JSON.stringify(localTools));
    if (url.includes("/api/dirs")) return new Response(JSON.stringify(localDirs));
    if (url.includes("/api/sessions") && (init?.method ?? "GET") === "POST")
      return new Response(JSON.stringify([{ id: 3, tmuxName: "mm-3", toolId: 1, dir: "/a", status: "running" }]), {
        status: 201,
      });
    return new Response("[]");
  });
  const onLaunched = vi.fn();

  render(<HeaderLauncher servers={servers} onLaunched={onLaunched} />);

  const toolSelect = await screen.findByLabelText<HTMLSelectElement>("tool");
  await waitFor(() => expect(toolSelect.value).toBe("1"));
  const button = screen.getByText<HTMLButtonElement>("+ New");
  expect(button).toBeEnabled();

  fireEvent.change(screen.getByLabelText("server"), { target: { value: "r1" } });

  // Tool/dir ids are per-daemon autoincrements: the local daemon's options must
  // not survive the switch, or a launch would POST id 1 to a daemon where it
  // means a different tool.
  expect(screen.queryByLabelText("tool")).toBeNull();
  expect(screen.queryByText("claude")).toBeNull();
  expect(screen.queryByText("multimux")).toBeNull();
  expect(button).toBeDisabled();
  // Empty lists mid-load are not "nothing configured": no misleading hint.
  expect(screen.queryByText(/add tools/)).toBeNull();
  expect(screen.queryByText(/add dirs/)).toBeNull();

  fireEvent.click(button);
  expect(fetchMock.mock.calls.some(([, init]) => init?.method === "POST")).toBe(false);
  expect(onLaunched).not.toHaveBeenCalled();

  // Once the remote fetch resolves empty, the Settings hint is correct.
  pending.forEach((resolve) => resolve());
  await waitFor(() => expect(screen.getByText(/add tools/)).toBeInTheDocument());
  expect(button).toBeDisabled();
});

// A dir id is a per-daemon autoincrement, so a server switch can hand back the
// very same id (both daemons' first configured dir is id 7 here). A forget
// whose DELETE fails after that switch must not mistake the new server's dir-7
// history for the one it started on.
test("switching servers before a failed forget lands leaves the other server's history alone", async () => {
  let releaseDelete: (() => void) | undefined;
  const deletePending = new Promise<void>((resolve) => {
    releaseDelete = resolve;
  });
  let deleteSettled = false;
  const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const url = String(input);
    if (url.includes("/children")) return new Response("[]");
    if (url.startsWith("http://remote.test")) {
      if (url.includes("/subdirs")) return new Response(JSON.stringify(["Downloads"]));
      if (url.includes("/api/tools")) return new Response(JSON.stringify(localTools));
      if (url.includes("/api/dirs"))
        return new Response(JSON.stringify([{ id: 7, name: "box-a-dir", path: "/remote" }]));
      return new Response("[]");
    }
    if (url.includes("/subdirs")) {
      if ((init?.method ?? "GET") === "DELETE") {
        await deletePending;
        deleteSettled = true;
        return new Response("nope", { status: 500 });
      }
      return new Response(JSON.stringify(["web/src"]));
    }
    if (url.includes("/api/tools")) return new Response(JSON.stringify(localTools));
    if (url.includes("/api/dirs")) return new Response(JSON.stringify(localDirs));
    return new Response("[]");
  });

  render(<HeaderLauncher servers={servers} onLaunched={vi.fn()} />);

  await openPicker();
  fireEvent.click(await screen.findByLabelText("forget /repos/multimux/web/src"));

  // Switch to the other server while dir 7's DELETE is still in flight on the
  // first one. The picker closes with the fields it sits beside.
  fireEvent.keyDown(await screen.findByRole("dialog"), { key: "Escape" });
  fireEvent.change(screen.getByLabelText("server"), { target: { value: "r1" } });
  await waitFor(() =>
    expect(fetchMock.mock.calls.some(([u]) => String(u).includes("remote.test/api/dirs/7/subdirs"))).toBe(true),
  );
  await openPicker();
  expect(await screen.findByText("/remote/Downloads")).toBeInTheDocument();

  // Let the stale DELETE fail now that the user has moved to another daemon.
  releaseDelete?.();
  await waitFor(() => expect(deleteSettled).toBe(true));

  expect(screen.queryByText(/couldn't forget/i)).toBeNull();
  expect(screen.getByText("/remote/Downloads")).toBeInTheDocument();
  expect(screen.queryByText("/repos/multimux/web/src")).toBeNull();
});

test("a failed launch keeps the picker open, reports the error, and can be retried", async () => {
  let postCount = 0;
  const fetchMock = mockDaemon({
    post: () => {
      postCount++;
      if (postCount === 1) return new Response(JSON.stringify({ error: "directory invalid" }), { status: 400 });
      return new Response(
        JSON.stringify([{ id: 3, tmuxName: "mm-3", toolId: 1, dir: "/repos/multimux", status: "running" }]),
        { status: 201 },
      );
    },
  });
  const onLaunched = vi.fn();
  render(<HeaderLauncher servers={[servers[0]]} onLaunched={onLaunched} />);

  await openPicker();
  fireEvent.click(screen.getByText("multimux"));

  expect(await screen.findByText(/launch failed/)).toBeInTheDocument();
  expect(screen.getByRole("dialog")).toBeInTheDocument();
  expect(onLaunched).not.toHaveBeenCalled();

  fireEvent.click(screen.getByText("multimux"));
  await waitFor(() => expect(onLaunched).toHaveBeenCalledTimes(1));
  expect(fetchMock.mock.calls.filter(([, init]) => init?.method === "POST")).toHaveLength(2);
});

// A tool whose command carries the group separator answers one launch with
// several sessions; every one of them has to reach the grid.
test("a group launch places every session it started and closes the picker", async () => {
  const group = [
    { id: 3, tmuxName: "mm-3", toolId: 1, dir: "/repos/multimux", status: "running", label: "zsh" },
    { id: 4, tmuxName: "mm-4", toolId: 1, dir: "/repos/multimux", status: "running", label: "claude" },
  ];
  mockDaemon({ post: () => new Response(JSON.stringify(group), { status: 201 }) });
  const onLaunched = vi.fn();
  render(<HeaderLauncher servers={[servers[0]]} onLaunched={onLaunched} />);

  await openPicker();
  fireEvent.click(screen.getByText("multimux"));

  await waitFor(() => expect(onLaunched).toHaveBeenCalledTimes(2));
  expect(onLaunched.mock.calls.map(([, sess]) => sess.id)).toEqual([3, 4]);
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
});
