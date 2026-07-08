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
