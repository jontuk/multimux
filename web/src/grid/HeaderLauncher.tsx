import { useState } from "react";
import type { Server } from "../servers";
import DirPicker from "./DirPicker";
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
  const [picking, setPicking] = useState(false);

  async function launch(dirId: number, subdir: string) {
    const batch = await launcher.launch(dirId, subdir);
    if (!batch) return;
    setPicking(false);
    for (const session of batch.sessions) onLaunched(batch.server, session);
  }

  return launcher.server ? (
    <div className="header-launcher">
      <SessionLauncherFields servers={servers} model={launcher} variant="desktop" />
      {/* The picker shows the model's errors while it is open, so the header
          says nothing a scrim is covering. */}
      {!picking && launcher.error && <span className="launcher-error">{launcher.error}</span>}
      {/* The picker hangs off the button that opens it rather than floating in
          the middle of the page: it is that button's menu, and the directory
          it lands on is what "+ New" will do. The scrim behind it is only a
          click-away target. */}
      <div className="launcher-anchor">
        <button
          className="launch"
          disabled={!launcher.canLaunch}
          title="launch a new session"
          onClick={() => setPicking(true)}
        >
          + New
        </button>
        {picking && (
          <>
            <div className="dir-picker-scrim" onMouseDown={() => setPicking(false)} />
            <div className="dir-picker-pop">
              <DirPicker
                server={launcher.server}
                dirs={launcher.dirs}
                recents={launcher.recents}
                start={launcher.start}
                busy={launcher.busy}
                error={launcher.error}
                onForget={(recent) => void launcher.forget(recent)}
                onLaunch={(dirId, subdir) => void launch(dirId, subdir)}
                onClose={() => setPicking(false)}
              />
            </div>
          </>
        )}
      </div>
    </div>
  ) : null;
}
