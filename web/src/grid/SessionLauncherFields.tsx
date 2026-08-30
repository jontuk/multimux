import type { KeyboardEvent, ReactNode } from "react";
import type { Server } from "../servers";
import type { SessionLauncherModel } from "./useSessionLauncher";

export default function SessionLauncherFields({
  servers,
  model,
  variant,
  onSubmit,
  onIdleEscape,
}: {
  servers: Server[];
  model: SessionLauncherModel;
  variant: "desktop" | "mobile";
  onSubmit: () => void;
  onIdleEscape?: () => void;
}) {
  const labelled = variant === "mobile";

  function subdirKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      if (model.options.length === 0) return;
      event.preventDefault();
      model.moveHighlight(event.key === "ArrowDown" ? 1 : -1);
    } else if (event.key === "Enter") {
      event.preventDefault();
      if (model.showMenu && model.highlighted >= 0) model.chooseOption(model.highlighted);
      else onSubmit();
    } else if (event.key === "Escape") {
      event.stopPropagation();
      if (model.suggestionsOpen) model.closeSuggestions();
      else if (onIdleEscape) onIdleEscape();
      else event.currentTarget.blur();
    }
  }

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
        <>
          {field(
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
          )}
          {field(
            "dir",
            <select
              aria-label="dir"
              value={model.dirId}
              onChange={(event) => model.selectDir(Number(event.target.value))}
            >
              {model.dirs.map((dir) => (
                <option key={dir.id} value={dir.id}>
                  {dir.name}
                </option>
              ))}
            </select>,
          )}
          <div className={`subdir-wrap${model.suggestionsOpen || model.subdir ? " slashed" : ""}`}>
            {(model.suggestionsOpen || model.subdir) && (
              <span className="subdir-slash" aria-hidden="true">
                /
              </span>
            )}
            <input
              className="subdir"
              aria-label="subdirectory"
              placeholder="subdir"
              value={model.subdir}
              spellCheck={false}
              autoCapitalize="off"
              autoCorrect="off"
              onFocus={model.openSuggestions}
              onBlur={model.closeSuggestions}
              onChange={(event) => model.changeSubdir(event.target.value)}
              onKeyDown={subdirKeyDown}
            />
            {model.showMenu && (
              <div className="subdir-history" onMouseDown={(event) => event.preventDefault()}>
                {model.options.map((option, index) => (
                  <div
                    key={option.value}
                    className={`subdir-history-row${index === model.highlighted ? " on" : ""}`}
                    onMouseEnter={model.clearHighlight}
                  >
                    <button type="button" className="subdir-pick" onClick={() => model.chooseOption(index)}>
                      {option.value}
                    </button>
                    {option.remembered ? (
                      <button
                        type="button"
                        className="subdir-forget"
                        aria-label={`forget ${option.value}`}
                        onClick={() => void model.forget(option.value)}
                      >
                        ×
                      </button>
                    ) : (
                      <span className="subdir-tag" aria-hidden="true">
                        dir
                      </span>
                    )}
                  </div>
                ))}
              </div>
            )}
          </div>
        </>
      )}
      {model.error && <span className="launcher-error">{model.error}</span>}
    </div>
  );
}
