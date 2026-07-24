import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { vi } from "vitest";
import ToolsPanel from "../settings/ToolsPanel";
import DirsPanel from "../settings/DirsPanel";

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
