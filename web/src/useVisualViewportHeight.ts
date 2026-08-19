import { useEffect, useState } from "react";

export const VIEWPORT_SETTLE_MS = 150;

export function useVisualViewportHeight(): number | null {
  const viewport = window.visualViewport;
  const [height, setHeight] = useState<number | null>(() => viewport?.height ?? null);

  useEffect(() => {
    if (!viewport) return;
    let settleTimer: ReturnType<typeof setTimeout> | null = null;
    const settle = () => {
      if (settleTimer) clearTimeout(settleTimer);
      settleTimer = setTimeout(() => {
        settleTimer = null;
        setHeight(viewport.height);
      }, VIEWPORT_SETTLE_MS);
    };

    viewport.addEventListener("resize", settle);
    return () => {
      viewport.removeEventListener("resize", settle);
      if (settleTimer) clearTimeout(settleTimer);
    };
  }, [viewport]);

  return height;
}
