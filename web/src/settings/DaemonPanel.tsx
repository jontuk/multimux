import { useCallback, useState } from "react";
import { apiFetch, errorText } from "../api";
import { localServer } from "../servers";
import { useFetch } from "../useFetch";
import PanelState from "./PanelState";

type Settings = { hostname: string; extraSans: string; port: string; version: string };
type SettingsResponse = {
  ok?: boolean;
  rpWarning?: boolean;
  restartRequired?: boolean;
  error?: string;
  rpChange?: boolean;
  credentials?: number;
};

export default function DaemonPanel() {
  const [hostname, setHostname] = useState("");
  const [extraSans, setExtraSans] = useState("");
  const [port, setPort] = useState("");
  const [rpWarning, setRpWarning] = useState(false);
  const [error, setError] = useState("");
  const [pendingRpChange, setPendingRpChange] = useState<number | null>(null);
  const [loading, setLoading] = useState(false);

  const seed = useCallback((s: Settings) => {
    setHostname(s.hostname);
    setExtraSans(s.extraSans);
    setPort(s.port);
  }, []);
  const {
    data: settings,
    error: loadError,
    loading: loadingSettings,
    reload,
  } = useFetch<Settings>("/api/settings", seed);

  async function save(confirmRpChange: boolean) {
    if (!settings) return;
    if (port !== "" && !/^\d+$/.test(port)) return;
    setError("");
    setPendingRpChange(null);
    try {
      setLoading(true);
      const res = await apiFetch(localServer(), "/api/settings", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ hostname, extraSans, port, confirmRpChange }),
      });
      const data = (await res.json()) as SettingsResponse;
      if (res.status === 409 && data.rpChange) {
        // The daemon refused: this hostname change alters the WebAuthn RP ID
        // and would strand every registered passkey. Ask before retrying.
        setPendingRpChange(data.credentials ?? 0);
        return;
      }
      if (!res.ok) {
        setError(data.error ?? `save failed (${res.status})`);
        return;
      }
      if (data.rpWarning) {
        setRpWarning(true);
      }
      reload();
    } catch (err) {
      setError(errorText(err));
    } finally {
      setLoading(false);
    }
  }

  return (
    <section>
      <h2>Daemon Settings</h2>
      <PanelState loading={loadingSettings} error={loadError} onRetry={reload} />
      {rpWarning && (
        <div className="server-status-banner">Changing hostname invalidates ALL passkeys after restart</div>
      )}
      {error && <div className="server-status-banner">{error}</div>}
      {pendingRpChange !== null && (
        <div className="server-status-banner">
          This hostname change invalidates {pendingRpChange} registered passkey(s) after restart.{" "}
          <button disabled={loading} onClick={() => save(true)}>
            Confirm hostname change
          </button>{" "}
          <button disabled={loading} onClick={() => setPendingRpChange(null)}>
            Cancel
          </button>
        </div>
      )}
      {settings && !loadingSettings && !loadError && (
        <>
          <div className="settings-fields">
            <div className="settings-readonly">
              <span>Version</span>
              <span className="daemon-version">{settings.version || "unknown"}</span>
            </div>
            <label>
              Hostname
              <input value={hostname} onChange={(e) => setHostname(e.target.value)} disabled={loading} />
            </label>
            <label>
              Extra SANs
              <input value={extraSans} onChange={(e) => setExtraSans(e.target.value)} disabled={loading} />
            </label>
            <label>
              Port
              <input type="number" value={port} onChange={(e) => setPort(e.target.value)} disabled={loading} />
            </label>
          </div>
          <button className="primary" disabled={loading} onClick={() => save(false)}>
            Save
          </button>
        </>
      )}
    </section>
  );
}
