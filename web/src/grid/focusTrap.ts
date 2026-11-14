import type { KeyboardEvent } from "react";

/**
 * Keeps Tab inside a modal dialog: from the last focusable element it wraps to
 * the first, and Shift+Tab the other way. Call it from the dialog's keydown
 * handler for the Tab key.
 */
export function trapTab(event: KeyboardEvent, dialog: HTMLElement | null) {
  const focusable = Array.from(
    dialog?.querySelectorAll<HTMLElement>('button:not(:disabled), [href], [tabindex]:not([tabindex="-1"])') ?? [],
  );
  if (focusable.length === 0) return;
  const first = focusable[0];
  const last = focusable[focusable.length - 1];
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault();
    last.focus();
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault();
    first.focus();
  }
}
