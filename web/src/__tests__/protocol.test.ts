import { encodeResize, parseServerText } from "../term/protocol";

test("encodeResize marks an active page", () => {
  expect(JSON.parse(encodeResize(120, 40, true))).toEqual({
    type: "resize",
    cols: 120,
    rows: 40,
    active: true,
  });
});

test("encodeResize marks an inactive page", () => {
  expect(JSON.parse(encodeResize(80, 24, false))).toEqual({
    type: "resize",
    cols: 80,
    rows: 24,
    active: false,
  });
});

test("parseServerText exit", () => {
  expect(parseServerText('{"type":"exit"}')).toEqual({ type: "exit" });
});

test("parseServerText garbage", () => {
  expect(parseServerText("not json")).toBeNull();
});
