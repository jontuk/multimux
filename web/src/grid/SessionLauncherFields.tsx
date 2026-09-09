import type { ReactNode } from "react";
import type { Server } from "../servers";
import type { SessionLauncherModel } from "./useSessionLauncher";

/**
 * The choices a launch needs that are not the directory: which daemon, which
 * tool. Where the session goes is the picker's job — these stay put between
 * launches, so they belong on the bar rather than inside a dialog.
 */
export default function SessionLauncherFields({
  servers,
  model,
  variant,
}: {
  servers: Server[];
  model: SessionLauncherModel;
  variant: "desktop" | "mobile";
}) {
  const labelled = variant === "mobile";

  const field = (name: string, control: ReactNode) => (
    <label className={`session-launcher-field session-launcher-field-${name}`}>
      {labelled && <span>{name}</span>}
      {control}
    </label>
  );

  return (
    <div className={`session-launcher-fields session-launcher-fields-${variant}`}>
      {servers.length > 1 &&
        field(
          "server",
          <select
            aria-label="server"
            value={model.serverId}
            onChange={(event) => model.selectServer(event.target.value)}
          >
            {servers.map((server) => (
              <option key={server.id} value={server.id}>
                {server.name}
              </option>
            ))}
          </select>,
        )}
      {model.loading ? (
        <span className="launcher-hint">loading…</span>
      ) : model.unconfigured ? (
        <span className="launcher-hint">
          add {model.unconfigured} in <a href="#/settings">Settings</a>
        </span>
      ) : (
        field(
          "tool",
          <select
            aria-label="tool"
            value={model.toolId}
            onChange={(event) => model.selectTool(Number(event.target.value))}
          >
            {model.tools.map((tool) => (
              <option key={tool.id} value={tool.id}>
                {tool.name}
              </option>
            ))}
          </select>,
        )
      )}
    </div>
  );
}
