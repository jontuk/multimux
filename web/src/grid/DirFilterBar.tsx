import { dirTintStyle } from "./dirColor";
import type { DirButton } from "./dirFilter";

// One pill per directory in use, wearing the same tint as the tile headers of
// the sessions it controls, so the pairing is visible rather than remembered.
// Presentational: the solo and the close live in GridPage.
//
// With nothing soloed every pill reads unpressed and undimmed — nothing is
// filtered, so nothing should look switched off.
//
// The two actions are sibling buttons inside the pill rather than one nested
// in the other: a button inside a button is invalid, and the states below are
// therefore carried by the wrapper so they cover both halves.
export default function DirFilterBar({
  dirs,
  solo,
  onSolo,
  onClose,
}: {
  dirs: DirButton[];
  solo: string | null;
  onSolo: (path: string) => void;
  onClose: (path: string) => void;
}) {
  if (dirs.length === 0) return null;
  return (
    <div className="dir-filter">
      {dirs.map((d) => {
        const on = solo === d.path;
        const off = solo !== null && !on;
        const action = on ? "show all directories" : `show only sessions in ${d.path}`;
        const closeAction = `close ${d.count} session${d.count === 1 ? "" : "s"} in ${d.path}`;
        return (
          <span
            key={d.path}
            className={`dir-filter-item${on ? " dir-filter-on" : off ? " dir-filter-off" : ""}`}
            style={dirTintStyle(d.path)}
          >
            <button
              className="dir-filter-solo"
              aria-pressed={on}
              // The visible text leads the accessible name rather than being
              // replaced by it: a screen reader still hears the leaf name and
              // count, and voice control can act on the label it can see
              // (WCAG 2.5.3).
              aria-label={`${d.name} ${d.count} — ${action}`}
              // The keyboard route is only in the tooltip: it belongs to the bar
              // as a whole, and repeating it in every accessible name would make
              // a screen reader read it once per button.
              title={`${action}\nCtrl+Alt+←/→ to rotate, Ctrl+Alt+0 to show all`}
              onClick={() => onSolo(d.path)}
            >
              {d.name}
              <span className="dir-filter-count">{d.count}</span>
            </button>
            <button
              className="dir-filter-close"
              // Nothing here is readable text, so the label replaces it rather
              // than leading it.
              aria-label={closeAction}
              title={closeAction}
              onClick={() => onClose(d.path)}
            >
              ×
            </button>
          </span>
        );
      })}
    </div>
  );
}
