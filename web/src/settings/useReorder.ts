import { useCallback, useMemo, useState } from "react";
import type { DragEvent, KeyboardEvent } from "react";
import { errorText, putJSON } from "../api";
import { localServer } from "../servers";

type Item = { id: number };

/** Result of moving one row: the new list, ready to render and to send. */
function moved<T extends Item>(items: T[], from: number, to: number): T[] {
  if (from === to || from < 0 || to < 0 || from >= items.length || to >= items.length) return items;
  const next = items.slice();
  const [row] = next.splice(from, 1);
  next.splice(to, 0, row);
  return next;
}

/**
 * Drag-and-keyboard reordering for a settings list.
 *
 * The list rendered while a save is in flight is the pending one, so the row
 * stays where the user dropped it instead of snapping back for a moment. A
 * failed save drops the pending order — `items` reverts on the next render —
 * and reports why.
 */
export function useReorder<T extends Item>(items: T[], path: string, reload: () => void) {
  const [pending, setPending] = useState<T[] | null>(null);
  const [dragIndex, setDragIndex] = useState<number | null>(null);
  const [overIndex, setOverIndex] = useState<number | null>(null);
  const [error, setError] = useState<string | null>(null);

  // A save that lands, a row added, a row deleted: whenever the server's list
  // matches the pending one by id, the pending copy has done its job.
  const ordered = useMemo(() => {
    if (!pending) return items;
    const same = pending.length === items.length && pending.every((p) => items.some((i) => i.id === p.id));
    return same ? pending : items;
  }, [pending, items]);

  const save = useCallback(
    async (next: T[]) => {
      setPending(next);
      setError(null);
      try {
        await putJSON(localServer(), path, { ids: next.map((i) => i.id) });
        reload();
      } catch (e: unknown) {
        setPending(null);
        setError(errorText(e));
        reload();
      }
    },
    [path, reload],
  );

  const move = useCallback(
    (from: number, to: number) => {
      const next = moved(ordered, from, to);
      if (next !== ordered) void save(next);
    },
    [ordered, save],
  );

  /** Props for the drag handle of the row at `index`. */
  const handleProps = useCallback(
    (index: number, label: string) => ({
      draggable: true,
      role: "button",
      tabIndex: 0,
      "aria-label": `reorder ${label}`,
      className: `drag-handle${overIndex === index ? " drag-over" : ""}`,
      onDragStart: (e: DragEvent) => {
        setDragIndex(index);
        e.dataTransfer.effectAllowed = "move";
        // Firefox only starts a drag once some data is set.
        e.dataTransfer.setData("text/plain", String(index));
      },
      onDragOver: (e: DragEvent) => {
        e.preventDefault();
        e.dataTransfer.dropEffect = "move";
        setOverIndex(index);
      },
      onDragLeave: () => setOverIndex((i) => (i === index ? null : i)),
      onDrop: (e: DragEvent) => {
        e.preventDefault();
        const raw = e.dataTransfer.getData("text/plain");
        const from = dragIndex ?? (raw === "" ? NaN : Number(raw));
        setDragIndex(null);
        setOverIndex(null);
        if (Number.isInteger(from)) move(from, index);
      },
      onDragEnd: () => {
        setDragIndex(null);
        setOverIndex(null);
      },
      onKeyDown: (e: KeyboardEvent) => {
        if (e.key !== "ArrowUp" && e.key !== "ArrowDown") return;
        e.preventDefault();
        move(index, index + (e.key === "ArrowUp" ? -1 : 1));
      },
    }),
    [dragIndex, overIndex, move],
  );

  return { ordered, handleProps, reorderError: error };
}
