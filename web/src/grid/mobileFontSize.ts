export const MOBILE_FONT_SIZES = [13, 11, 10, 9] as const;
export type MobileFontSize = (typeof MOBILE_FONT_SIZES)[number];
export const DEFAULT_MOBILE_FONT_SIZE: MobileFontSize = 13;

const KEY = "multimux.mobileFontSize";

function isMobileFontSize(value: unknown): value is MobileFontSize {
  return typeof value === "number" && MOBILE_FONT_SIZES.some((size) => size === value);
}

export function readMobileFontSize(): MobileFontSize {
  try {
    const value: unknown = JSON.parse(localStorage.getItem(KEY) ?? "null");
    return isMobileFontSize(value) ? value : DEFAULT_MOBILE_FONT_SIZE;
  } catch {
    return DEFAULT_MOBILE_FONT_SIZE;
  }
}

export function writeMobileFontSize(size: MobileFontSize): void {
  try {
    localStorage.setItem(KEY, JSON.stringify(size));
  } catch {
    // The in-memory selection remains useful when storage is unavailable.
  }
}
