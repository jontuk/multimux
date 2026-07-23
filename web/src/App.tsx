import { useCallback, useEffect, useState } from "react";
import LoginPage from "./pages/LoginPage";
import SetupPage from "./pages/SetupPage";
import SettingsPage from "./pages/SettingsPage";
import ConnectPage from "./pages/ConnectPage";
import { errorText, getJSON, isUnauthorized, isUnreachable } from "./api";
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

/**
 * Startup only sends you to the login page when the daemon actually said
 * "unauthenticated". An unreachable or broken daemon gets its own screen —
 * a passkey prompt cannot fix either of those.
 */
type Startup =
  | { kind: "loading" }
  | { kind: "ready" }
  | { kind: "unauthed" }
  | { kind: "unreachable" }
  | { kind: "error"; detail: string };

function problem(e: unknown): Startup {
  return isUnreachable(e) ? { kind: "unreachable" } : { kind: "error", detail: errorText(e) };
}

function StartupProblem({ state, onRetry }: { state: Startup; onRetry: () => void }) {
  const unreachable = state.kind === "unreachable";
  return (
    <div className="app-loading app-startup-problem">
      <h1>{unreachable ? "Can't reach the multimux daemon" : "The daemon returned an error"}</h1>
      <p>
        {unreachable
          ? "It may be stopped, or this browser can't get to it. Start it and try again."
          : state.kind === "error"
            ? state.detail
            : ""}
      </p>
      <button className="primary" onClick={onRetry}>
        Retry
      </button>
    </div>
  );
}

export default function App() {
  const [health, setHealth] = useState<Health | null>(null);
  const [startup, setStartup] = useState<Startup>({ kind: "loading" });
  const [route, setRoute] = useState(window.location.hash || "#/");
  const [headerSlot, setHeaderSlot] = useState<HTMLElement | null>(null);

  useEffect(() => {
    document.title = health?.hostLabel ? `multimux @${health.hostLabel}` : "multimux";
  }, [health?.hostLabel]);

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

  const check = useCallback(
    () =>
      getJSON<Health>(localServer(), "/healthz")
        .then((h) => {
          setHealth(h);
          return getJSON(localServer(), "/api/auth/me").then(
            () => setStartup({ kind: "ready" }),
            // Only the daemon saying "unauthenticated" earns the login page.
            (e: unknown) => setStartup(isUnauthorized(e) ? { kind: "unauthed" } : problem(e)),
          );
        })
        .catch((e: unknown) => {
          setHealth(null);
          setStartup(problem(e));
        }),
    [],
  );

  const retry = useCallback(() => {
    setStartup({ kind: "loading" });
    void check();
  }, [check]);

  useEffect(() => {
    void check();
  }, [check]);

  if (window.location.pathname === "/setup" || health?.setupPending) return <SetupPage />;
  if (startup.kind === "unauthed") return <LoginPage />;
  if (startup.kind === "unreachable" || startup.kind === "error")
    return <StartupProblem state={startup} onRetry={retry} />;
  if (startup.kind === "loading") return <div className="app-loading">multimux loading…</div>;

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
