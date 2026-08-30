import { useEffect } from "react";
import type { Server } from "../servers";
import SessionLauncherFields from "./SessionLauncherFields";
import type { Session } from "./types";
import { useSessionLauncher } from "./useSessionLauncher";

export default function MobileSessionCreator({
  servers,
  initialServerId,
  targetDir = null,
  targetServerId = null,
  onCancel,
  onLaunched,
}: {
  servers: Server[];
  initialServerId?: string | null;
  targetDir?: string | null;
  targetServerId?: string | null;
  onCancel: () => void;
  onLaunched: (server: Server, sessions: Session[]) => void;
}) {
  const launcher = useSessionLauncher({ servers, initialServerId, targetDir, targetServerId });

  useEffect(() => {
    const close = (event: KeyboardEvent) => {
      if (event.key === "Escape") onCancel();
    };
    window.addEventListener("keydown", close);
    return () => window.removeEventListener("keydown", close);
  }, [onCancel]);

  async function submit() {
    const batch = await launcher.launch();
    if (batch) onLaunched(batch.server, batch.sessions);
  }

  return (
    <section className="mobile-session-creator" aria-label="New session">
      <header>
        <button type="button" autoFocus aria-label="Close new session creator" onClick={onCancel}>
          ←
        </button>
        <h1>New session</h1>
      </header>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          void submit();
        }}
      >
        <SessionLauncherFields
          servers={servers}
          model={launcher}
          variant="mobile"
          onSubmit={() => void submit()}
          onIdleEscape={onCancel}
        />
        <button className="primary mobile-create-session" type="submit" disabled={!launcher.canLaunch}>
          Create session
        </button>
      </form>
    </section>
  );
}
