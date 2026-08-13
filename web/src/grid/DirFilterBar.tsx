import { dirTintStyle } from "./dirColor";
import type { DirButton } from "./dirFilter";

// One button per directory in use, wearing the same tint as the tile headers
// of the sessions it controls, so the pairing is visible rather than
// remembered. Presentational: the solo lives in GridPage.
//
// With nothing soloed every button reads unpressed and undimmed — nothing is
// filtered, so nothing should look switched off.
export default function DirFilterBar({
  dirs,
  solo,
  onSolo,
}: {
  dirs: DirButton[];
  solo: string | null;
  onSolo: (path: string) => void;
}) {
  if (dirs.length === 0) return null;
  return (
    <div className="dir-filter">
      {dirs.map((d) => {
        const on = solo === d.path;
        const off = solo !== null && !on;
        const action = on ? "show all directories" : `show only sessions in ${d.path}`;
        return (
          <button
            key={d.path}
            className={off ? "dir-filter-off" : undefined}
            style={dirTintStyle(d.path)}
            aria-pressed={on}
            // The visible text leads the accessible name rather than being
            // replaced by it: a screen reader still hears the leaf name and
            // count, and voice control can act on the label it can see
            // (WCAG 2.5.3).
            aria-label={`${d.name} ${d.count} — ${action}`}
            title={action}
            onClick={() => onSolo(d.path)}
          >
            {d.name}
            <span className="dir-filter-count">{d.count}</span>
          </button>
        );
      })}
    </div>
  );
}
