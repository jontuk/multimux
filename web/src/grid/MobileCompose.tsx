import { createPortal } from "react-dom";
import { useEffect, useId, useRef, useState, type RefObject } from "react";
import type { TerminalHandle } from "../term/TerminalTile";

export default function MobileCompose({
  terminalRef,
  controlsSlot,
}: {
  terminalRef: RefObject<TerminalHandle | null>;
  controlsSlot: HTMLElement | null;
}) {
  const panelId = useId();
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState("");
  const [status, setStatus] = useState("");

  useEffect(() => {
    if (open) textareaRef.current?.focus();
  }, [open]);

  function insert(sendEnter: boolean) {
    if (!draft) return;
    setStatus("");
    const terminal = terminalRef.current;
    if (!terminal?.paste(draft)) {
      setStatus("Terminal is disconnected. Draft not sent.");
      return;
    }
    if (sendEnter) terminal.input("\r");
    setDraft("");
    setOpen(false);
  }

  const toggle = (
    <button
      className="mobile-compose-toggle"
      type="button"
      aria-controls={panelId}
      aria-expanded={open}
      onClick={() => {
        setStatus("");
        setOpen((current) => !current);
      }}
    >
      Compose
    </button>
  );

  return (
    <>
      {controlsSlot && createPortal(toggle, controlsSlot)}
      {open && (
        <section className="mobile-compose" id={panelId} aria-label="Compose terminal input">
          <label htmlFor={`${panelId}-textarea`}>Compose terminal input</label>
          <textarea
            id={`${panelId}-textarea`}
            ref={textareaRef}
            value={draft}
            rows={4}
            onChange={(event) => {
              setDraft(event.target.value);
              setStatus("");
            }}
          />
          <div className="mobile-compose-actions">
            <button type="button" disabled={!draft} onClick={() => insert(false)}>
              Insert
            </button>
            <button type="button" disabled={!draft} onClick={() => insert(true)}>
              Insert &amp; Enter
            </button>
          </div>
          {status && <p role="status">{status}</p>}
        </section>
      )}
    </>
  );
}
