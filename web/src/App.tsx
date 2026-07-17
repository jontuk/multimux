import { useEffect, useState } from "react";
import LoginPage from "./pages/LoginPage";
import SetupPage from "./pages/SetupPage";
import SettingsPage from "./pages/SettingsPage";
import ConnectPage from "./pages/ConnectPage";
import { apiFetch, getJSON } from "./api";
import { localServer } from "./servers";
import GridPage from "./grid/GridPage";

type Health = { status: string; setupPending: boolean; version: string };

export default function App() {
  const [health, setHealth] = useState<Health | null>(null);
  const [authed, setAuthed] = useState<boolean | null>(null);
  const [route, setRoute] = useState(window.location.hash || "#/");

  useEffect(() => {
    const onHash = () => setRoute(window.location.hash || "#/");
    window.addEventListener("hashchange", onHash);
    return () => window.removeEventListener("hashchange", onHash);
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
      <header>
        <a href="#/" className="wordmark">
          <span className="prompt">~</span>multimux
        </a>
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
        {route === "#/" && <GridPage />}
        {route === "#/settings" && <SettingsPage />}
        {route.startsWith("#/connect") && <ConnectPage />}
      </main>
    </div>
  );
}
