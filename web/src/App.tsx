import { useEffect, useState } from "react";
import LoginPage from "./pages/LoginPage";
import SetupPage from "./pages/SetupPage";
import SettingsPage from "./pages/SettingsPage";
import ConnectPage from "./pages/ConnectPage";
import { apiFetch, getJSON } from "./api";
import { localServer } from "./servers";
import GridPage from "./grid/GridPage";
import { APPEARANCE_EVENT, type AppearanceDetail } from "./settings/AppearancePanel";

type Health = {
  status: string;
  setupPending: boolean;
  version: string;
  hostLabel?: string;
  accentColor?: string;
};

export default function App() {
  const [health, setHealth] = useState<Health | null>(null);
  const [authed, setAuthed] = useState<boolean | null>(null);
  const [route, setRoute] = useState(window.location.hash || "#/");
  const [headerSlot, setHeaderSlot] = useState<HTMLElement | null>(null);

  useEffect(() => {
    const onHash = () => setRoute(window.location.hash || "#/");
    window.addEventListener("hashchange", onHash);
    return () => window.removeEventListener("hashchange", onHash);
  }, []);

  useEffect(() => {
    const onAppearance = (e: Event) => {
      const detail = (e as CustomEvent<AppearanceDetail>).detail;
      setHealth((h) => (h ? { ...h, hostLabel: detail.hostLabel, accentColor: detail.accentColor } : h));
    };
    window.addEventListener(APPEARANCE_EVENT, onAppearance);
    return () => window.removeEventListener(APPEARANCE_EVENT, onAppearance);
  }, []);

  useEffect(() => {
    getJSON<Health>(localServer(), "/healthz")
      .then(setHealth)
      .catch(() => setHealth(null));
    apiFetch(localServer(), "/api/auth/me")
      .then((r) => setAuthed(r.ok))
      .catch(() => setAuthed(false));
  }, []);

  if (window.location.pathname === "/setup" || health?.setupPending) return <SetupPage />;
  if (authed === false) return <LoginPage />;
  if (authed === null) return <div className="app-loading">multimux loading…</div>;

  // Settings (Task 23), Connect (Task 24) routed here.
  return (
    <div className="app">
      <header
        className={health?.accentColor ? "host-accented" : undefined}
        style={health?.accentColor ? ({ "--host-accent": health.accentColor } as React.CSSProperties) : undefined}
      >
        <a href="#/" className="wordmark">
          <span className="prompt">~</span>multimux
        </a>
        {health?.hostLabel && <span className="host-label">@{health.hostLabel}</span>}
        {/* GridPage portals its launcher + shape picker here while the grid route is active. */}
        <div id="header-controls" ref={setHeaderSlot} />
        <nav>
          <a href="#/" className={route === "#/" ? "active" : ""}>
            Grid
          </a>
          <a href="#/settings" className={route === "#/settings" ? "active" : ""}>
            Settings
          </a>
        </nav>
      </header>
      <main id="page-root">
        {route === "#/" && <GridPage headerSlot={headerSlot} />}
        {route === "#/settings" && <SettingsPage />}
        {route.startsWith("#/connect") && <ConnectPage />}
      </main>
    </div>
  );
}
