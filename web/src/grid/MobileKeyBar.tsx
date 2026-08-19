import type { RefObject } from "react";
import type { TerminalHandle } from "../term/TerminalTile";

const terminalKeys: ReadonlyArray<{ label: string; accessibleName?: string; input: string }> = [
  { label: "Esc", input: "\x1b" },
  { label: "Tab", input: "\t" },
  { label: "Ctrl-C", input: "\x03" },
  { label: "←", accessibleName: "Left", input: "\x1b[D" },
  { label: "↑", accessibleName: "Up", input: "\x1b[A" },
  { label: "↓", accessibleName: "Down", input: "\x1b[B" },
  { label: "→", accessibleName: "Right", input: "\x1b[C" },
  { label: "Enter", input: "\r" },
];

export default function MobileKeyBar({ terminalRef }: { terminalRef: RefObject<TerminalHandle | null> }) {
  return (
    <div className="mobile-key-bar" role="group" aria-label="Terminal keys">
      {terminalKeys.map((key) => (
        <button
          key={key.label}
          type="button"
          aria-label={key.accessibleName}
          onPointerDown={(event) => event.preventDefault()}
          onClick={() => terminalRef.current?.input(key.input)}
        >
          {key.label}
        </button>
      ))}
    </div>
  );
}
