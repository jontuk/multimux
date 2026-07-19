export type Server = { id: string; origin: string; name: string; token?: string };

const KEY = "multimux.servers";

export function localServer(): Server {
  return { id: "local", origin: window.location.origin, name: "local" };
}

function loadStored(): Server[] {
  try {
    return JSON.parse(localStorage.getItem(KEY) ?? "[]") as Server[];
  } catch {
    return [];
  }
}

function save(servers: Server[]) {
  localStorage.setItem(KEY, JSON.stringify(servers));
}

export function listServers(): Server[] {
  return [localServer(), ...loadStored()];
}

export function addServer(origin: string, name: string): Server {
  const s: Server = { id: crypto.randomUUID(), origin: origin.replace(/\/$/, ""), name };
  save([...loadStored(), s]);
  return s;
}

export function setServerToken(id: string, token: string) {
  save(loadStored().map((s) => (s.id === id ? { ...s, token } : s)));
}

export function removeServer(id: string) {
  save(loadStored().filter((s) => s.id !== id));
}

// Opens the remote daemon's consent popup; on approval the popup posts the
// token back and it replaces the stored one. `onToken` fires after the store
// is updated so callers can re-read listServers().
export function connectServer(server: Server, onToken?: () => void) {
  const popup = window.open(`${server.origin}/#/connect?opener=${encodeURIComponent(window.location.origin)}`);
  let pollId: ReturnType<typeof setInterval> | null = null;
  function cleanup() {
    window.removeEventListener("message", onMsg);
    if (pollId) clearInterval(pollId);
  }
  function onMsg(ev: MessageEvent) {
    if (ev.origin !== server.origin || ev.data?.type !== "multimux-token") return;
    setServerToken(server.id, ev.data.token);
    cleanup();
    popup?.close();
    onToken?.();
  }
  window.addEventListener("message", onMsg);
  // If the popup was blocked (null) or the user closes/denies it, stop
  // listening so listeners don't stack across repeated Connect clicks.
  if (!popup) {
    cleanup();
    return;
  }
  pollId = setInterval(() => {
    if (popup.closed) cleanup();
  }, 500);
}
