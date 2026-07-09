import { useCallback, useEffect, useState } from "react";
import { getJSON } from "../api";
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
      const res = await fetch(localServer().origin + "/api/settings", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        credentials: "same-origin",
        body: JSON.stringify({
          hostname,
          extraSans,
          port,
        }),
      });
      if (!res.ok) throw new Error(`${res.status}`);
      const data = (await res.json()) as SettingsResponse;
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
        <div style={{ color: "red", border: "1px solid red", padding: "8px", marginBottom: "8px" }}>
          Changing hostname invalidates ALL passkeys after restart
        </div>
      )}
      <div>
        <label>
          Hostname:
          <input value={hostname} onChange={(e) => setHostname(e.target.value)} disabled={loading} />
        </label>
      </div>
      <div>
        <label>
          Extra SANs:
          <input value={extraSans} onChange={(e) => setExtraSans(e.target.value)} disabled={loading} />
        </label>
      </div>
      <div>
        <label>
          Port:
          <input type="number" value={port} onChange={(e) => setPort(e.target.value)} disabled={loading} />
        </label>
      </div>
      <button disabled={loading} onClick={save}>
        Save
      </button>
    </section>
  );
}
