import { vi } from "vitest";
import { dragHasFiles, imageFiles, quotePath, uploadImage } from "../term/upload";
import { ApiError } from "../api";

function transfer(files: File[], types: string[] = ["Files"]): DataTransfer {
  return { files, items: [], types } as unknown as DataTransfer;
}

afterEach(() => vi.restoreAllMocks());

test("imageFiles keeps only uploadable images", () => {
  const png = new File(["x"], "a.png", { type: "image/png" });
  const pdf = new File(["x"], "a.pdf", { type: "application/pdf" });
  const svg = new File(["x"], "a.svg", { type: "image/svg+xml" });
  expect(imageFiles(transfer([png, pdf, svg]))).toEqual([png]);
  expect(imageFiles(null)).toEqual([]);
});

test("imageFiles falls back to clipboard items when files is empty", () => {
  const png = new File(["x"], "image.png", { type: "image/png" });
  const data = {
    files: [],
    items: [
      { kind: "string", getAsFile: () => null },
      { kind: "file", getAsFile: () => png },
    ],
    types: ["Files"],
  } as unknown as DataTransfer;
  expect(imageFiles(data)).toEqual([png]);
});

test("dragHasFiles distinguishes file drags from text drags", () => {
  expect(dragHasFiles(transfer([], ["Files"]))).toBe(true);
  expect(dragHasFiles(transfer([], ["text/plain"]))).toBe(false);
  expect(dragHasFiles(null)).toBe(false);
});

test("quotePath escapes shell specials like a terminal file drop", () => {
  expect(quotePath("/home/u/.local/share/multimux/uploads/paste-1.png")).toBe(
    "/home/u/.local/share/multimux/uploads/paste-1.png",
  );
  expect(quotePath("/Users/j d/it's (1).png")).toBe("/Users/j\\ d/it\\'s\\ \\(1\\).png");
});

test("uploadImage posts the raw image with its type and returns the saved path", async () => {
  const fetchSpy = vi
    .spyOn(globalThis, "fetch")
    .mockResolvedValue(new Response(JSON.stringify({ path: "/up/paste-1.png" }), { status: 201 }));
  const png = new File(["x"], "a.png", { type: "image/png" });
  const server = { id: "r", origin: "https://remote:8686", name: "remote", token: "tok" };

  await expect(uploadImage(server, png)).resolves.toBe("/up/paste-1.png");
  const [url, init] = fetchSpy.mock.calls[0] as [string, RequestInit];
  expect(url).toBe("https://remote:8686/api/uploads");
  expect(init.method).toBe("POST");
  expect(init.body).toBe(png);
  expect(init.headers).toMatchObject({ "Content-Type": "image/png", Authorization: "Bearer tok" });
});

test("uploadImage surfaces the daemon's error", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(
    new Response(JSON.stringify({ error: "image larger than 32 MiB" }), { status: 413 }),
  );
  const server = { id: "l", origin: "https://local", name: "local" };
  const err = await uploadImage(server, new File(["x"], "a.png", { type: "image/png" })).catch((e) => e);
  expect(err).toBeInstanceOf(ApiError);
  expect((err as ApiError).status).toBe(413);
  expect((err as ApiError).detail).toBe("image larger than 32 MiB");
});
