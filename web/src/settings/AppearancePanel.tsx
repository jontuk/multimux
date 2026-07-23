import { useCallback, useState } from "react";
import { errorText, putJSON } from "../api";
import { localServer } from "../servers";
import { useFetch } from "../useFetch";
import PanelState from "./PanelState";

type Appearance = { hostLabel: string; accentColor: string; osHostname: string };

/** Fired after a save so App can update the header without refetching /healthz. */
export const APPEARANCE_EVENT = "multimux:appearance";
export type AppearanceDetail = { hostLabel: string; accentColor: string };

export default function AppearancePanel() {
  const [hostLabel, setHostLabel] = useState("");
  const [accentColor, setAccentColor] = useState("");
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState("");

  const seed = useCallback((a: Appearance) => {
    setHostLabel(a.hostLabel);
    setAccentColor(a.accentColor);
  }, []);
  const { data: appearance, error, loading, reload } = useFetch<Appearance>("/api/settings/appearance", seed);

  async function save() {
    if (!appearance) return;
    setSaveError("");
    try {
      setSaving(true);
      await putJSON(localServer(), "/api/settings/appearance", { hostLabel, accentColor });
      window.dispatchEvent(
        new CustomEvent<AppearanceDetail>(APPEARANCE_EVENT, {
          detail: { hostLabel: hostLabel || appearance.osHostname, accentColor },
        }),
      );
      reload();
    } catch (err) {
      setSaveError(errorText(err));
    } finally {
      setSaving(false);
    }
  }

  return (
    <section>
      <h2>Appearance</h2>
      <p className="settings-hint">
        Shown in the header bar so you can tell this host apart from other multimux instances.
      </p>
      <PanelState loading={loading} error={error} onRetry={reload} />
      {saveError && <div className="server-status-banner">{saveError}</div>}
      {appearance && !loading && !error && (
        <>
          <div className="settings-fields">
            <label>
              Host label
              <input
                value={hostLabel}
                placeholder={appearance.osHostname}
                onChange={(e) => setHostLabel(e.target.value)}
                maxLength={64}
                disabled={saving}
              />
            </label>
            <label>
              Accent colour
              <span className="accent-picker">
                <input
                  type="color"
                  aria-label="accent colour"
                  value={accentColor || "#000000"}
                  onChange={(e) => setAccentColor(e.target.value)}
                  disabled={saving}
                />
                {accentColor && (
                  <button type="button" className="link" onClick={() => setAccentColor("")} disabled={saving}>
                    clear
                  </button>
                )}
              </span>
            </label>
          </div>
          <button className="primary" disabled={saving} onClick={save}>
            Save
          </button>
        </>
      )}
    </section>
  );
}
