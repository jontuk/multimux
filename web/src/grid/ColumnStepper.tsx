import { MAX_COLS, MIN_COLS } from "./model";

export default function ColumnStepper({
  cols,
  rows,
  onChange,
}: {
  cols: number;
  rows: number;
  onChange: (cols: number) => void;
}) {
  return (
    <div className="column-stepper" title={`${cols}×${rows} grid`}>
      <button aria-label="fewer columns" disabled={cols <= MIN_COLS} onClick={() => onChange(cols - 1)}>
        ‹
      </button>
      <span className="stepper-icon" aria-hidden="true" style={{ gridTemplateColumns: `repeat(${cols}, 5px)` }}>
        {Array.from({ length: rows * cols }).map((_, i) => (
          <span key={i} className="cell" />
        ))}
      </span>
      <button aria-label="more columns" disabled={cols >= MAX_COLS} onClick={() => onChange(cols + 1)}>
        ›
      </button>
    </div>
  );
}
