import { useRef } from "react";
import { setColSizes, setRowSizes, type Layout } from "./model";
import { equalSizes, resizeTracks, MIN_TILE_PX, SNAP_PX } from "./sizes";
import { beginReflowHold, endReflowHold } from "../term/reflowGate";

type Props = {
  layout: Layout;
  containerRef: React.RefObject<HTMLDivElement | null>;
  /** Live update during the drag — state only, no network. */
  onPreview: (next: Layout) => void;
  /** Drag finished (or a double-click reset) — persist. */
  onCommit: (next: Layout) => void;
};

type Drag = {
  axis: "row" | "col";
  row: number;
  boundary: number;
  /** Container size on the drag axis, in px, captured at pointerdown. */
  extent: number;
  origin: number;
  start: number[];
};

export default function GridDividers({ layout, containerRef, onPreview, onCommit }: Props) {
  const drag = useRef<Drag | null>(null);

  const rowSizes = layout.rowSizes ?? equalSizes(layout.shape.rows);
  const colSizes = layout.colSizes ?? [];

  function apply(d: Drag, clientPos: number, commit: boolean) {
    const delta = (clientPos - d.origin) / d.extent;
    const min = MIN_TILE_PX / d.extent;
    const snap = SNAP_PX / d.extent;
    const next = resizeTracks(d.start, d.boundary, delta, min, snap);
    const layoutNext = d.axis === "row" ? setRowSizes(layout, next) : setColSizes(layout, d.row, next);
    (commit ? onCommit : onPreview)(layoutNext);
  }

  function startDrag(e: React.PointerEvent, axis: "row" | "col", row: number, boundary: number) {
    const box = containerRef.current?.getBoundingClientRect();
    if (!box) return;
    // A splitter drag is not a tile drag: stop the tile's HTML5 dragstart and
    // the terminal underneath from seeing this pointer at all.
    e.preventDefault();
    e.stopPropagation();
    (e.target as HTMLElement).setPointerCapture(e.pointerId);
    drag.current = {
      axis,
      row,
      boundary,
      extent: axis === "row" ? box.height : box.width,
      origin: axis === "row" ? e.clientY : e.clientX,
      start: axis === "row" ? rowSizes.slice() : (colSizes[row] ?? equalSizes(layout.shape.cols)).slice(),
    };
    beginReflowHold();
  }

  function moveDrag(e: React.PointerEvent) {
    const d = drag.current;
    if (!d) return;
    apply(d, d.axis === "row" ? e.clientY : e.clientX, false);
  }

  function endDrag(e: React.PointerEvent) {
    const d = drag.current;
    if (!d) return;
    drag.current = null;
    apply(d, d.axis === "row" ? e.clientY : e.clientX, true);
    // Release after the commit so the tiles are at their final size when each
    // terminal does its single catch-up refit.
    endReflowHold();
  }

  function reset(axis: "row" | "col", row: number) {
    onCommit(
      axis === "row"
        ? setRowSizes(layout, equalSizes(layout.shape.rows))
        : setColSizes(layout, row, equalSizes(layout.shape.cols)),
    );
  }

  const handles: React.ReactNode[] = [];

  // Row dividers: full width, at each row boundary.
  let top = 0;
  for (let r = 0; r < layout.shape.rows - 1; r++) {
    top += rowSizes[r];
    const at = top * 100;
    handles.push(
      <div
        key={`row-${r}`}
        data-divider={`row-${r}`}
        className="grid-divider grid-divider-row"
        role="separator"
        aria-orientation="horizontal"
        style={{ top: `${at}%` }}
        onPointerDown={(e) => startDrag(e, "row", 0, r)}
        onPointerMove={moveDrag}
        onPointerUp={endDrag}
        onPointerCancel={endDrag}
        onDoubleClick={() => reset("row", 0)}
      />,
    );
  }

  // Column dividers: one set per row, spanning only that row's height.
  let rowTop = 0;
  for (let r = 0; r < layout.shape.rows; r++) {
    const widths = colSizes[r] ?? equalSizes(layout.shape.cols);
    let left = 0;
    for (let c = 0; c < layout.shape.cols - 1; c++) {
      left += widths[c];
      handles.push(
        <div
          key={`col-${r}-${c}`}
          data-divider={`col-${r}-${c}`}
          className="grid-divider grid-divider-col"
          role="separator"
          aria-orientation="vertical"
          style={{ left: `${left * 100}%`, top: `${rowTop * 100}%`, height: `${rowSizes[r] * 100}%` }}
          onPointerDown={(e) => startDrag(e, "col", r, c)}
          onPointerMove={moveDrag}
          onPointerUp={endDrag}
          onPointerCancel={endDrag}
          onDoubleClick={() => reset("col", r)}
        />,
      );
    }
    rowTop += rowSizes[r];
  }

  return <>{handles}</>;
}
