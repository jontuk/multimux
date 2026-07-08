import { encodeResize, parseServerText } from "../term/protocol";

test("encodeResize", () => {
  expect(JSON.parse(encodeResize(120, 40))).toEqual({ type: "resize", cols: 120, rows: 40 });
});

test("parseServerText exit", () => {
  expect(parseServerText('{"type":"exit"}')).toEqual({ type: "exit" });
});

test("parseServerText garbage", () => {
  expect(parseServerText("not json")).toBeNull();
});
