import { dirTintStyle } from "./dirColor";
import type { DirButton } from "./dirFilter";

// One button per directory in use, wearing the same tint as the tile headers
// of the sessions it controls, so the pairing is visible rather than
// remembered. Presentational: the hidden set lives in GridPage.
export default function DirFilterBar({
  dirs,
  hidden,
  onToggle,
}: {
  dirs: DirButton[];
  hidden: Set<string>;
  onToggle: (path: string) => void;
}) {
  if (dirs.length === 0) return null;
  return (
    <div className="dir-filter">
      {dirs.map((d) => {
        const off = hidden.has(d.path);
        return (
          <button
            key={d.path}
            className={off ? "dir-filter-off" : undefined}
            style={dirTintStyle(d.path)}
            aria-pressed={!off}
            title={`${off ? "show" : "hide"} sessions in ${d.path}`}
            onClick={() => onToggle(d.path)}
          >
            {d.name}
            <span className="dir-filter-count">{d.count}</span>
          </button>
        );
      })}
    </div>
  );
}
