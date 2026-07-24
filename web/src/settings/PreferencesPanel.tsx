import { useCallback, useState } from "react";
import { errorText, putJSON } from "../api";
import { localServer } from "../servers";
import { useFetch } from "../useFetch";
import PanelState from "./PanelState";

export type Preferences = { confirmTerminate: boolean };

/** Fired after a save so the grid honours the new setting without a reload. */
export const PREFERENCES_EVENT = "multimux:preferences";
export type PreferencesDetail = Preferences;

export default function PreferencesPanel() {
  const [confirmTerminate, setConfirmTerminate] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState("");

  const seed = useCallback((p: Preferences) => {
    setConfirmTerminate(p.confirmTerminate);
  }, []);
  const { data: prefs, error, loading, reload } = useFetch<Preferences>("/api/settings/preferences", seed);

  async function save() {
    if (!prefs) return;
    setSaveError("");
    try {
      setSaving(true);
      await putJSON(localServer(), "/api/settings/preferences", { confirmTerminate });
      window.dispatchEvent(new CustomEvent<PreferencesDetail>(PREFERENCES_EVENT, { detail: { confirmTerminate } }));
      reload();
    } catch (err) {
      setSaveError(errorText(err));
    } finally {
      setSaving(false);
    }
  }

  return (
    <section>
      <h2>Preferences</h2>
      <p className="settings-hint">
        Behaviour of this daemon's web UI. Also settable from the shell with <code>multimux config</code>.
      </p>
      <PanelState loading={loading} error={error} onRetry={reload} />
      {saveError && <div className="server-status-banner">{saveError}</div>}
      {prefs && !loading && !error && (
        <>
          <div className="settings-fields">
            <label>
              <input
                type="checkbox"
                checked={confirmTerminate}
                onChange={(e) => setConfirmTerminate(e.target.checked)}
                disabled={saving}
              />
              Ask before terminating a session
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
