import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { DragEvent, KeyboardEvent } from "react";
import { errorText, putJSON } from "../api";
import { localServer } from "../servers";

type Item = { id: number };

/**
 * An order the user asked for. `landedOn` is null while its save is in flight,
 * then the list that was current when the save landed.
 */
type Pending<T> = { ids: number[]; landedOn: T[] | null };

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
 * While a save is in flight, and until the reload after it delivers, `items`
 * is rendered in the pending order, so the row stays where the user dropped it
 * instead of snapping back for a moment. Only the order is pending: the rows
 * themselves always come from `items`, and once the reloaded list arrives it
 * is the only order again. A failed save drops the pending order — `items`
 * reverts on the next render — and reports why.
 */
export function useReorder<T extends Item>(items: T[], path: string, reload: () => void) {
  const [pending, setPending] = useState<Pending<T> | null>(null);
  const [dragIndex, setDragIndex] = useState<number | null>(null);
  const [overIndex, setOverIndex] = useState<number | null>(null);
  const [error, setError] = useState<string | null>(null);

  const current = useRef(items);
  useEffect(() => {
    current.current = items;
  });

  // The pending order has done its job once its save has landed and a newer
  // list has arrived, and never applies to a list with other rows in it (one
  // added or deleted meanwhile).
  const ordered = useMemo(() => {
    if (!pending || (pending.landedOn && pending.landedOn !== items)) return items;
    const byId = new Map(items.map((i) => [i.id, i]));
    if (pending.ids.length !== byId.size || !pending.ids.every((id) => byId.has(id))) return items;
    return pending.ids.map((id) => byId.get(id)!);
  }, [pending, items]);

  const save = useCallback(
    async (ids: number[]) => {
      setPending({ ids, landedOn: null });
      setError(null);
      // A later move replaces the pending order; this save must then leave it alone.
      const mine = (p: Pending<T> | null) => p?.ids === ids;
      try {
        await putJSON(localServer(), path, { ids });
        setPending((p) => (mine(p) ? { ids, landedOn: current.current } : p));
      } catch (e: unknown) {
        setPending((p) => (mine(p) ? null : p));
        setError(errorText(e));
      }
      reload();
    },
    [path, reload],
  );

  const move = useCallback(
    (from: number, to: number) => {
      const next = moved(ordered, from, to);
      if (next !== ordered) void save(next.map((i) => i.id));
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
