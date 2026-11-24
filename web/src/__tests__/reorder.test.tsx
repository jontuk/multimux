import { act, fireEvent, render, renderHook, screen, waitFor } from "@testing-library/react";
import type { KeyboardEvent } from "react";
import { vi } from "vitest";
import ToolsPanel from "../settings/ToolsPanel";
import DirsPanel from "../settings/DirsPanel";
import { useReorder } from "../settings/useReorder";

const threeTools = [
  { id: 1, name: "zsh", command: "zsh" },
  { id: 2, name: "claude", command: "claude" },
  { id: 3, name: "codex", command: "codex" },
];

/** Names in the order the table renders them. */
function renderedNames(): string[] {
  return screen
    .getAllByRole("row")
    .map((row) => row.querySelectorAll("td")[1]?.textContent ?? "")
    .filter(Boolean);
}

type FetchMock = { mock: { calls: Parameters<typeof fetch>[] } };

function orderRequests(fetchMock: FetchMock): unknown[] {
  return fetchMock.mock.calls
    .filter(([url, init]) => String(url).endsWith("/order") && init?.method === "PUT")
    .map(([, init]) => JSON.parse(init?.body as string));
}

/** Drag the handle at `from` onto the handle at `to`. */
function drag(from: number, to: number) {
  const handles = screen.getAllByLabelText(/^reorder /);
  const data = new Map<string, string>();
  const dataTransfer = {
    effectAllowed: "",
    dropEffect: "",
    setData: (k: string, v: string) => data.set(k, v),
    getData: (k: string) => data.get(k) ?? "",
  };
  fireEvent.dragStart(handles[from], { dataTransfer });
  fireEvent.dragOver(handles[to], { dataTransfer });
  fireEvent.drop(handles[to], { dataTransfer });
}

test("dropping a tool onto another row saves the new order", async () => {
  const reordered = [threeTools[2], threeTools[0], threeTools[1]];
  const fetchMock = vi
    .spyOn(globalThis, "fetch")
    .mockResolvedValueOnce(new Response(JSON.stringify(threeTools)))
    .mockResolvedValueOnce(new Response(null, { status: 204 }))
    .mockResolvedValueOnce(new Response(JSON.stringify(reordered)));

  render(<ToolsPanel />);
  await screen.findByLabelText("reorder zsh");
  expect(renderedNames()).toEqual(["zsh", "claude", "codex"]);

  drag(2, 0); // codex to the top

  await waitFor(() => expect(orderRequests(fetchMock)).toEqual([{ ids: [3, 1, 2] }]));
  const [url] = fetchMock.mock.calls[1];
  expect(String(url)).toContain("/api/tools/order");
  await waitFor(() => expect(renderedNames()).toEqual(["codex", "zsh", "claude"]));
  fetchMock.mockRestore();
});

test("arrow keys on a handle move that row", async () => {
  const reordered = [threeTools[1], threeTools[0], threeTools[2]];
  const fetchMock = vi
    .spyOn(globalThis, "fetch")
    .mockResolvedValueOnce(new Response(JSON.stringify(threeTools)))
    .mockResolvedValueOnce(new Response(null, { status: 204 }))
    .mockResolvedValueOnce(new Response(JSON.stringify(reordered)));

  render(<ToolsPanel />);
  await screen.findByLabelText("reorder claude");

  fireEvent.keyDown(screen.getByLabelText("reorder claude"), { key: "ArrowUp" });

  await waitFor(() => expect(orderRequests(fetchMock)).toEqual([{ ids: [2, 1, 3] }]));
  await waitFor(() => expect(renderedNames()).toEqual(["claude", "zsh", "codex"]));
  fetchMock.mockRestore();
});

test("an arrow key at the end of the list saves nothing", async () => {
  const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValueOnce(new Response(JSON.stringify(threeTools)));

  render(<ToolsPanel />);
  await screen.findByLabelText("reorder zsh");
  fireEvent.keyDown(screen.getByLabelText("reorder zsh"), { key: "ArrowUp" });
  fireEvent.keyDown(screen.getByLabelText("reorder codex"), { key: "ArrowDown" });

  expect(orderRequests(fetchMock)).toEqual([]);
  expect(renderedNames()).toEqual(["zsh", "claude", "codex"]);
  fetchMock.mockRestore();
});

test("a rejected reorder restores the old order and explains why", async () => {
  const fetchMock = vi
    .spyOn(globalThis, "fetch")
    .mockResolvedValueOnce(new Response(JSON.stringify(threeTools)))
    .mockResolvedValueOnce(
      new Response(JSON.stringify({ error: "order must list every id exactly once" }), {
        status: 400,
      }),
    )
    // The reload after the failure: the server's order, unchanged.
    .mockResolvedValueOnce(new Response(JSON.stringify(threeTools)));

  render(<ToolsPanel />);
  await screen.findByLabelText("reorder zsh");

  drag(2, 0);

  await screen.findByText(/order must list every id exactly once/);
  await waitFor(() => expect(renderedNames()).toEqual(["zsh", "claude", "codex"]));
  fetchMock.mockRestore();
});

test("directories reorder through /api/dirs/order", async () => {
  const dirs = [
    { id: 7, name: "repos", path: "/repos" },
    { id: 8, name: "tmp", path: "/tmp" },
  ];
  const fetchMock = vi
    .spyOn(globalThis, "fetch")
    .mockResolvedValueOnce(new Response(JSON.stringify(dirs)))
    .mockResolvedValueOnce(new Response(null, { status: 204 }))
    .mockResolvedValueOnce(new Response(JSON.stringify([dirs[1], dirs[0]])));

  render(<DirsPanel />);
  await screen.findByLabelText("reorder repos");

  drag(1, 0);

  await waitFor(() => expect(orderRequests(fetchMock)).toEqual([{ ids: [8, 7] }]));
  expect(String(fetchMock.mock.calls[1][0])).toContain("/api/dirs/order");
  await waitFor(() => expect(renderedNames()).toEqual(["tmp", "repos"]));
  fetchMock.mockRestore();
});

test("an edit made after a reorder shows once the list reloads", async () => {
  const reordered = [threeTools[2], threeTools[0], threeTools[1]];
  const renamed = [{ ...threeTools[2], name: "codex-next" }, threeTools[0], threeTools[1]];
  const fetchMock = vi
    .spyOn(globalThis, "fetch")
    .mockResolvedValueOnce(new Response(JSON.stringify(threeTools)))
    .mockResolvedValueOnce(new Response(null, { status: 204 }))
    .mockResolvedValueOnce(new Response(JSON.stringify(reordered)))
    .mockResolvedValueOnce(new Response(null, { status: 204 }))
    .mockResolvedValueOnce(new Response(JSON.stringify(renamed)));

  render(<ToolsPanel />);
  await screen.findByLabelText("reorder zsh");
  drag(2, 0);
  await waitFor(() => expect(renderedNames()).toEqual(["codex", "zsh", "claude"]));
  await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(3));

  fireEvent.click((await screen.findAllByText("edit"))[0]);
  fireEvent.change(screen.getByLabelText("edit name"), { target: { value: "codex-next" } });
  fireEvent.click(screen.getByText("save"));

  await waitFor(() => expect(renderedNames()).toEqual(["codex-next", "zsh", "claude"]));
  fetchMock.mockRestore();
});

/** A PUT the test answers when it chooses. */
function deferredPut() {
  let settle!: (res: Response) => void;
  const promise = new Promise<Response>((resolve) => (settle = resolve));
  return { promise, ok: () => settle(new Response(null, { status: 204 })) };
}

function renderReorder() {
  const reload = vi.fn();
  const hook = renderHook(({ items }) => useReorder(items, "/api/tools/order", reload), {
    initialProps: { items: threeTools },
  });
  const press = (index: number, key: string) =>
    act(() => {
      const event = { key, preventDefault: () => {} } as KeyboardEvent;
      hook.result.current.handleProps(index, "row").onKeyDown(event);
    });
  const names = () => hook.result.current.ordered.map((t) => t.name);
  return { ...hook, reload, press, names };
}

test("a moved row stays put from the drop until the reloaded list arrives", async () => {
  const put = deferredPut();
  const fetchMock = vi.spyOn(globalThis, "fetch").mockReturnValueOnce(put.promise);
  const { rerender, reload, press, names } = renderReorder();

  press(2, "ArrowUp");
  expect(names()).toEqual(["zsh", "codex", "claude"]);

  // The save lands, but the reload it asked for has not answered yet.
  await act(async () => put.ok());
  expect(reload).toHaveBeenCalledTimes(1);
  expect(names()).toEqual(["zsh", "codex", "claude"]);

  rerender({ items: [threeTools[0], threeTools[2], threeTools[1]] });
  expect(names()).toEqual(["zsh", "codex", "claude"]);

  // From here on the server's list is the only order: another tab's reorder shows.
  rerender({ items: [threeTools[1], threeTools[2], threeTools[0]] });
  expect(names()).toEqual(["claude", "codex", "zsh"]);
  fetchMock.mockRestore();
});

test("a second move made while the first is saving is the one that stays", async () => {
  const first = deferredPut();
  const second = deferredPut();
  const fetchMock = vi
    .spyOn(globalThis, "fetch")
    .mockClear() // the spy keeps earlier tests' calls
    .mockReturnValueOnce(first.promise)
    .mockReturnValueOnce(second.promise);
  const { rerender, press, names } = renderReorder();

  press(2, "ArrowUp"); // zsh codex claude
  press(1, "ArrowUp"); // codex zsh claude
  expect(orderRequests(fetchMock)).toEqual([{ ids: [1, 3, 2] }, { ids: [3, 1, 2] }]);
  expect(names()).toEqual(["codex", "zsh", "claude"]);

  // The first save and its reload complete while the second is still in flight.
  await act(async () => first.ok());
  rerender({ items: [threeTools[0], threeTools[2], threeTools[1]] });
  expect(names()).toEqual(["codex", "zsh", "claude"]);

  await act(async () => second.ok());
  expect(names()).toEqual(["codex", "zsh", "claude"]);
  rerender({ items: [threeTools[2], threeTools[0], threeTools[1]] });
  expect(names()).toEqual(["codex", "zsh", "claude"]);
  fetchMock.mockRestore();
});

test("a failed save puts the row back and reports why", async () => {
  const fetchMock = vi
    .spyOn(globalThis, "fetch")
    .mockResolvedValueOnce(new Response(JSON.stringify({ error: "nope" }), { status: 400 }));
  const { result, reload, press, names } = renderReorder();

  press(2, "ArrowUp");
  expect(names()).toEqual(["zsh", "codex", "claude"]);

  await waitFor(() => expect(result.current.reorderError).toMatch(/nope/));
  expect(names()).toEqual(["zsh", "claude", "codex"]);
  expect(reload).toHaveBeenCalledTimes(1);
  fetchMock.mockRestore();
});
