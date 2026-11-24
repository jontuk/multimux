export type Server = { id: string; origin: string; name: string; token?: string };

const KEY = "multimux.servers";

export function localServer(): Server {
  return { id: "local", origin: window.location.origin, name: "local" };
}

/** What the user typed is not something a daemon could be reached at. */
export class InvalidOriginError extends Error {
  constructor(input: string) {
    super(`"${input}" is not a server address — expected something like https://otherbox:8686`);
    this.name = "InvalidOriginError";
  }
}

// The origin of an http(s) URL, or null. Everything that talks to a server
// builds its URLs by string concatenation on `origin`, so anything else here
// ends as a URL `new WebSocket` throws on or `fetch` sends somewhere odd.
function httpOrigin(raw: string): string | null {
  let url: URL;
  try {
    url = new URL(raw);
  } catch {
    return null;
  }
  if (url.protocol !== "https:" && url.protocol !== "http:") return null;
  // Not something a server address has; it is what "mailto:me@box" turns into
  // once https:// has been assumed in front of it.
  if (url.username || url.password) return null;
  return url.origin;
}

/**
 * Turns what the user typed into the origin to store: path, query and trailing
 * slash dropped, host lowercased. Throws InvalidOriginError when there is no
 * http(s) origin in it.
 *
 * A bare `host` or `host:port` gets https://, which is all a daemon serves.
 * That case has to be spotted before parsing: `new URL("mux.local:8686")`
 * succeeds, reading "mux.local:" as the scheme.
 */
export function normalizeOrigin(input: string): string {
  const typed = input.trim();
  const origin = typed && httpOrigin(/^[a-z][a-z0-9+.-]*:\/\//i.test(typed) ? typed : `https://${typed}`);
  if (!origin) throw new InvalidOriginError(typed);
  return origin;
}

// Whatever is in storage, as far as it has the shape of a server. Origins are
// returned as stored, usable or not — see listServers and invalidServers.
// localStorage is hand-editable and outlives the code that wrote it, so
// nothing about it is taken on trust: a list that is not an array reads as
// empty, and an entry without its string fields is skipped, there being
// nothing to show it as or remove it by.
function loadStored(): Server[] {
  let parsed: unknown;
  try {
    parsed = JSON.parse(localStorage.getItem(KEY) ?? "[]");
  } catch {
    return [];
  }
  if (!Array.isArray(parsed)) return [];
  return parsed.flatMap((entry: unknown): Server[] => {
    if (typeof entry !== "object" || entry === null) return [];
    const { id, origin, name, token } = entry as Record<string, unknown>;
    if (typeof id !== "string" || typeof origin !== "string" || typeof name !== "string") return [];
    return [{ id, origin, name, ...(typeof token === "string" ? { token } : {}) }];
  });
}

function save(servers: Server[]) {
  localStorage.setItem(KEY, JSON.stringify(servers));
}

// Every server that can be connected to. A stored entry whose origin is not an
// http(s) one is left out, so the grid, the events socket and the terminals
// never see it: addServer once stored whatever was typed, and one such entry
// took the whole page down on every load.
export function listServers(): Server[] {
  return [
    localServer(),
    ...loadStored().flatMap((s) => {
      const origin = httpOrigin(s.origin);
      return origin ? [{ ...s, origin }] : [];
    }),
  ];
}

// The stored entries listServers leaves out. They stay in storage, and
// Settings lists them, so the user can see what was typed and remove it —
// rather than a server they added vanishing without a word. No scheme is
// assumed for these as it is for fresh input: quietly turning one into a
// server the grid connects to is not this module's call.
export function invalidServers(): Server[] {
  return loadStored().filter((s) => httpOrigin(s.origin) === null);
}

// Throws InvalidOriginError, having stored nothing, when `origin` is unusable.
export function addServer(origin: string, name: string): Server {
  const s: Server = { id: crypto.randomUUID(), origin: normalizeOrigin(origin), name };
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
