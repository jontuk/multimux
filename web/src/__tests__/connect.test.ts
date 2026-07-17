import { parseOpenerOrigin } from "../pages/openerOrigin";

test("valid https opener", () => {
  expect(parseOpenerOrigin("?opener=https%3A%2F%2Fbox-a%3A8686")).toBe("https://box-a:8686");
});

test("rejects garbage", () => {
  expect(parseOpenerOrigin("?opener=javascript%3Aalert(1)")).toBeNull();
  expect(parseOpenerOrigin("")).toBeNull();
});

test("allows http localhost for dev", () => {
  expect(parseOpenerOrigin("?opener=http%3A%2F%2Flocalhost%3A5173")).toBe("http://localhost:5173");
});
