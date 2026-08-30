import { afterEach, expect, test, vi } from "vitest";
import {
  DEFAULT_MOBILE_FONT_SIZE,
  MOBILE_FONT_SIZES,
  readMobileFontSize,
  writeMobileFontSize,
} from "../grid/mobileFontSize";

afterEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
});

test("defines the supported mobile terminal font sizes and default", () => {
  expect(MOBILE_FONT_SIZES).toEqual([13, 11, 10, 9]);
  expect(DEFAULT_MOBILE_FONT_SIZE).toBe(13);
});

test("reads the default when no mobile font size is stored", () => {
  expect(readMobileFontSize()).toBe(13);
});

test("round-trips a supported mobile font size", () => {
  writeMobileFontSize(10);
  expect(localStorage.getItem("multimux.mobileFontSize")).toBe("10");
  expect(readMobileFontSize()).toBe(10);
});

test("ignores malformed and unsupported stored font sizes", () => {
  for (const value of ["{oops", "12", '"11"', "null"]) {
    localStorage.setItem("multimux.mobileFontSize", value);
    expect(readMobileFontSize()).toBe(13);
  }
});

test("storage failures fall back on read and do not fail writes", () => {
  vi.spyOn(Storage.prototype, "getItem").mockImplementationOnce(() => {
    throw new Error("unavailable");
  });
  expect(readMobileFontSize()).toBe(13);

  vi.spyOn(Storage.prototype, "setItem").mockImplementationOnce(() => {
    throw new Error("full");
  });
  expect(() => writeMobileFontSize(11)).not.toThrow();
});
