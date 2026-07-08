import { SHAPES, type GridShape } from "./model";

export default function ShapePicker({ value, onChange }: { value: GridShape; onChange: (s: GridShape) => void }) {
  return (
    <div className="shape-picker">
      {SHAPES.map((s) => {
        const active = s.rows === value.rows && s.cols === value.cols;
        return (
          <button
            key={`${s.rows}x${s.cols}`}
            title={`${s.rows}×${s.cols}`}
            className={active ? "shape active" : "shape"}
            onClick={() => onChange(s)}
          >
            <span
              className="shape-cells"
              style={{ display: "grid", gridTemplateColumns: `repeat(${s.cols}, 6px)`, gap: 1 }}
            >
              {Array.from({ length: s.rows * s.cols }).map((_, i) => (
                <span key={i} className="cell" style={{ width: 6, height: 6, background: "currentColor" }} />
              ))}
            </span>
          </button>
        );
      })}
    </div>
  );
}
