import type { Server } from "../servers";
import SessionLauncherFields from "./SessionLauncherFields";
import type { Session } from "./types";
import { useSessionLauncher } from "./useSessionLauncher";

export default function HeaderLauncher({
  servers,
  targetDir = null,
  targetServerId = null,
  onLaunched,
}: {
  servers: Server[];
  targetDir?: string | null;
  targetServerId?: string | null;
  onLaunched: (server: Server, session: Session) => void;
}) {
  const launcher = useSessionLauncher({ servers, targetDir, targetServerId });

  async function launch() {
    const batch = await launcher.launch();
    if (!batch) return;
    for (const session of batch.sessions) onLaunched(batch.server, session);
  }

  return launcher.server ? (
    <div className="header-launcher">
      <SessionLauncherFields
        servers={servers}
        model={launcher}
        variant="desktop"
        onSubmit={() => void launch()}
      />
      <button
        className="launch"
        disabled={!launcher.canLaunch}
        title="launch a new session"
        onClick={() => void launch()}
      >
        + New
      </button>
    </div>
  ) : null;
}
