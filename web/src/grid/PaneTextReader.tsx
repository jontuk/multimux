import { useCallback, useEffect, useId, useLayoutEffect, useRef, useState, type KeyboardEvent } from "react";
import { cleanPaneText, errorText, type PaneTextResult } from "../api";
import type { Server } from "../servers";

type Cleanup = Pick<PaneTextResult, "processor" | "model" | "warning">;

function cleanupStatus(cleanup: Cleanup | null): string {
  if (!cleanup) return "";
  if (cleanup.warning) return cleanup.warning;
  if (cleanup.processor === "raw") return "No cleanup needed.";
  const name = cleanup.processor === "codex" ? "Codex" : "Claude";
  return `Cleaned with ${name} (${cleanup.model}).`;
}

type Props = {
  server: Server;
  sessionId: number;
  title: string;
  open: boolean;
  onClose: () => void;
  trigger: HTMLElement | null;
};

export default function PaneTextReader({ server, sessionId, title, open, onClose, trigger }: Props) {
  const [snapshot, setSnapshot] = useState<string | null>(null);
  const [cleanup, setCleanup] = useState<Cleanup | null>(null);
  const [requestError, setRequestError] = useState("");
  const [refreshing, setRefreshing] = useState(false);
  const [copyStatus, setCopyStatus] = useState("");
  const [scrollGeneration, setScrollGeneration] = useState(0);
  const titleId = useId();
  const dialogRef = useRef<HTMLDivElement>(null);
  const closeRef = useRef<HTMLButtonElement>(null);
  const contentRef = useRef<HTMLPreElement>(null);
  const abortRef = useRef<AbortController | null>(null);
  const generationRef = useRef(0);
  const snapshotRef = useRef<string | null>(null);

  const invalidateRequests = useCallback((clearController = false) => {
    ++generationRef.current;
    abortRef.current?.abort();
    if (clearController) abortRef.current = null;
  }, []);

  const load = useCallback(async () => {
    const generation = ++generationRef.current;
    abortRef.current?.abort();
    const controller = new AbortController();
    abortRef.current = controller;
    const hadSnapshot = snapshotRef.current !== null;
    setRefreshing(hadSnapshot);
    setRequestError("");
    setCopyStatus("");
    try {
      const result = await cleanPaneText(server, sessionId, controller.signal);
      if (controller.signal.aborted || generation !== generationRef.current) return;
      snapshotRef.current = result.text;
      setSnapshot(result.text);
      setCleanup({ processor: result.processor, model: result.model, warning: result.warning });
      setScrollGeneration((value) => value + 1);
    } catch (err) {
      if (controller.signal.aborted || generation !== generationRef.current) return;
      setRequestError(errorText(err));
    } finally {
      if (!controller.signal.aborted && generation === generationRef.current) setRefreshing(false);
    }
  }, [server, sessionId]);

  useEffect(() => {
    if (!open) return;
    void load();
    closeRef.current?.focus();
    return () => {
      invalidateRequests(true);
      snapshotRef.current = null;
      setSnapshot(null);
      setCleanup(null);
      setRequestError("");
      setRefreshing(false);
      setCopyStatus("");
      trigger?.focus();
    };
  }, [invalidateRequests, load, open, trigger]);

  useLayoutEffect(() => {
    if (scrollGeneration > 0 && contentRef.current) {
      contentRef.current.scrollTop = contentRef.current.scrollHeight;
    }
  }, [scrollGeneration]);

  function onKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    event.stopPropagation();
    if (event.key === "Escape") {
      event.preventDefault();
      close();
      return;
    }
    if (event.key !== "Tab") return;
    const focusable = Array.from(
      dialogRef.current?.querySelectorAll<HTMLElement>(
        'button:not(:disabled), [href], [tabindex]:not([tabindex="-1"])',
      ) ?? [],
    );
    if (focusable.length === 0) return;
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  }

  async function copyAll() {
    const capturedSnapshot = snapshotRef.current;
    if (!capturedSnapshot) return;
    const generation = generationRef.current;
    try {
      if (!navigator.clipboard?.writeText) throw new Error("clipboard unavailable");
      await navigator.clipboard.writeText(capturedSnapshot);
      if (generation !== generationRef.current || capturedSnapshot !== snapshotRef.current) return;
      setCopyStatus("Copied pane text.");
    } catch {
      if (generation !== generationRef.current || capturedSnapshot !== snapshotRef.current) return;
      setCopyStatus("Clipboard unavailable. Select the text and copy it manually.");
    }
  }

  function close() {
    invalidateRequests();
    snapshotRef.current = null;
    setSnapshot(null);
    setCleanup(null);
    setRequestError("");
    setCopyStatus("");
    onClose();
  }

  if (!open) return null;
  const loading = snapshot === null && requestError === "";
  const initialFailure = snapshot === null && requestError !== "";
  const refreshError = snapshot !== null ? requestError : "";
  const activityStatus = refreshing ? "Refreshing and cleaning…" : copyStatus;
  const disclosure = cleanupStatus(cleanup);
  return (
    <div className="pane-text-backdrop">
      <div
        ref={dialogRef}
        className="pane-text-reader"
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        onKeyDown={onKeyDown}
      >
        <header className="pane-text-header">
          <h1 id={titleId}>Pane text for {title}</h1>
          <div className="pane-text-actions">
            <button type="button" onClick={() => void load()} disabled={refreshing}>
              Refresh
            </button>
            <button
              type="button"
              onClick={() => void copyAll()}
              disabled={refreshing || snapshot === null || snapshot.length === 0}
            >
              Copy all
            </button>
            <button ref={closeRef} type="button" onClick={close}>
              Close
            </button>
          </div>
        </header>
        <div className="pane-text-feedback" aria-live="polite">
          {disclosure && <span className={cleanup?.warning ? "error" : undefined}>{disclosure}</span>}
          {disclosure && refreshError && " "}
          {refreshError && <span className="error">{refreshError}</span>}
          {(disclosure || refreshError) && activityStatus && " "}
          {activityStatus && <span>{activityStatus}</span>}
        </div>
        {loading ? (
          <div className="pane-text-loading" role="status">
            Capturing and cleaning pane text…
          </div>
        ) : initialFailure ? (
          <div className="pane-text-initial-error">
            <p className="error">{requestError}</p>
            <div>
              <button type="button" onClick={() => void load()}>
                Retry
              </button>
              <button type="button" onClick={close}>
                Close
              </button>
            </div>
          </div>
        ) : (
          <pre ref={contentRef} className="pane-text-content" data-testid="pane-text-content" tabIndex={0}>
            {snapshot}
          </pre>
        )}
      </div>
    </div>
  );
}
