import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";
import GridPage from "../grid/GridPage";

vi.mock("../useEvents", () => ({ useEvents: vi.fn() }));
vi.mock("../term/TerminalTile", () => ({
  default: ({ sessionId }: { sessionId: number }) => <div data-testid={`term-${sessionId}`} />,
}));

const sessions = [
  { id: 1, tmuxName: "mm-1", toolId: 1, dir: "/a", status: "running" },
  { id: 2, tmuxName: "mm-2", toolId: 1, dir: "/b", status: "running" },
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
