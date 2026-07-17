import { useCallback, useEffect, useState } from "react";
import { getJSON, putJSON } from "../api";
import { localServer } from "../servers";

type Settings = { hostname: string; extraSans: string; port: string };
type SettingsResponse = { ok: boolean; rpWarning?: boolean; restartRequired?: boolean };

export default function DaemonPanel() {
  const [settings, setSettings] = useState<Settings | null>(null);
  const [hostname, setHostname] = useState("");
  const [extraSans, setExtraSans] = useState("");
  const [port, setPort] = useState("");
  const [rpWarning, setRpWarning] = useState(false);
  const [loading, setLoading] = useState(false);

  const refresh = useCallback(() => {
    getJSON<Settings>(localServer(), "/api/settings")
      .then((s) => {
        setSettings(s);
        setHostname(s.hostname);
        setExtraSans(s.extraSans);
        setPort(s.port);
      })
      .catch(() => setSettings(null));
  }, []);
  useEffect(refresh, [refresh]);

  async function save() {
    if (!settings) return;
    if (!/^\d+$/.test(port)) return;
    try {
      setLoading(true);
      const data = await putJSON<SettingsResponse>(localServer(), "/api/settings", { hostname, extraSans, port });
      if (data.rpWarning) {
        setRpWarning(true);
      }
      refresh();
    } catch (error) {
      console.error("Failed to save settings:", error);
    } finally {
      setLoading(false);
    }
  }

  if (!settings) return <div>Loading daemon settings…</div>;

  return (
    <section>
      <h2>Daemon Settings</h2>
      {rpWarning && (
        <div className="server-status-banner">Changing hostname invalidates ALL passkeys after restart</div>
      )}
      <div className="settings-fields">
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
      <button className="primary" disabled={loading} onClick={save}>
        Save
      </button>
    </section>
  );
}
