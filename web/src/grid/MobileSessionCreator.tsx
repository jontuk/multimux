import { useEffect } from "react";
import type { Server } from "../servers";
import DirPicker from "./DirPicker";
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

  async function launch(dirId: number, subdir: string) {
    const batch = await launcher.launch(dirId, subdir);
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
      <SessionLauncherFields servers={servers} model={launcher} variant="mobile" />
      {launcher.server && !launcher.loading && !launcher.unconfigured && (
        <DirPicker
          server={launcher.server}
          dirs={launcher.dirs}
          recents={launcher.recents}
          start={launcher.start}
          busy={launcher.busy}
          error={launcher.error}
          variant="mobile"
          onForget={(recent) => void launcher.forget(recent)}
          onLaunch={(dirId, subdir) => void launch(dirId, subdir)}
          onClose={onCancel}
        />
      )}
    </section>
  );
}
