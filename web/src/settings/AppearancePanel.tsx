import { useCallback, useEffect, useState } from "react";
import { getJSON, putJSON } from "../api";
import { localServer } from "../servers";

type Appearance = { hostLabel: string; accentColor: string; osHostname: string };

/** Fired after a save so App can update the header without refetching /healthz. */
export const APPEARANCE_EVENT = "multimux:appearance";
export type AppearanceDetail = { hostLabel: string; accentColor: string };

export default function AppearancePanel() {
  const [appearance, setAppearance] = useState<Appearance | null>(null);
  const [hostLabel, setHostLabel] = useState("");
  const [accentColor, setAccentColor] = useState("");
  const [loading, setLoading] = useState(false);

  const refresh = useCallback(() => {
    getJSON<Appearance>(localServer(), "/api/settings/appearance")
      .then((a) => {
        setAppearance(a);
        setHostLabel(a.hostLabel);
        setAccentColor(a.accentColor);
      })
      .catch(() => setAppearance(null));
  }, []);
  useEffect(refresh, [refresh]);

  async function save() {
    if (!appearance) return;
    try {
      setLoading(true);
      await putJSON(localServer(), "/api/settings/appearance", { hostLabel, accentColor });
      window.dispatchEvent(
        new CustomEvent<AppearanceDetail>(APPEARANCE_EVENT, {
          detail: { hostLabel: hostLabel || appearance.osHostname, accentColor },
        }),
      );
      refresh();
    } catch (error) {
      console.error("Failed to save appearance:", error);
    } finally {
      setLoading(false);
    }
  }

  if (!appearance) return <div>Loading appearance settings…</div>;

  return (
    <section>
      <h2>Appearance</h2>
      <p className="settings-hint">
        Shown in the header bar so you can tell this host apart from other multimux instances.
      </p>
      <div className="settings-fields">
        <label>
          Host label
          <input
            value={hostLabel}
            placeholder={appearance.osHostname}
            onChange={(e) => setHostLabel(e.target.value)}
            maxLength={64}
            disabled={loading}
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
              disabled={loading}
            />
            {accentColor && (
              <button type="button" className="link" onClick={() => setAccentColor("")} disabled={loading}>
                clear
              </button>
            )}
          </span>
        </label>
      </div>
      <button className="primary" disabled={loading} onClick={save}>
        Save
      </button>
    </section>
  );
}
