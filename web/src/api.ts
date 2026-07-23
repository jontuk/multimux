import type { Server } from "./servers";

/**
 * A failed request. `status` is the HTTP status, or 0 when the request never
 * got an answer at all (daemon down, TLS refused, DNS failure) — the case the
 * UI must not confuse with "not signed in". `detail` is the daemon's own error
 * text when it sent one; Go handlers reply `{"error": "..."}` (writeJSON) or a
 * bare text body (http.Error).
 */
export class ApiError extends Error {
  readonly status: number;
  readonly detail: string;
  constructor(status: number, path: string, detail = "") {
    super(status === 0 ? `${path}: daemon unreachable` : `${path}: ${status}${detail ? ` ${detail}` : ""}`);
    this.name = "ApiError";
    this.status = status;
    this.detail = detail;
  }
}

/** The daemon answered, but the caller is not (or no longer) authenticated. */
export function isUnauthorized(e: unknown): boolean {
  return e instanceof ApiError && (e.status === 401 || e.status === 403);
}

/** The daemon never answered. */
export function isUnreachable(e: unknown): boolean {
  return e instanceof ApiError && e.status === 0;
}

/** One short sentence fit for showing a human, whatever went wrong. */
export function errorText(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 0) return "Can't reach the daemon.";
    if (isUnauthorized(e)) return "Not signed in — reload to sign in again.";
    return e.detail ? `The daemon returned an error (${e.status}): ${e.detail}` : `The daemon returned ${e.status}.`;
  }
  return e instanceof Error ? e.message : String(e);
}

async function failed(path: string, res: Response): Promise<ApiError> {
  let detail = "";
  try {
    detail = (await res.text()).trim();
  } catch {
    // Body unreadable; the status alone still tells the caller what happened.
  }
  if (detail.startsWith("{")) {
    try {
      const parsed = JSON.parse(detail) as { error?: unknown };
      if (typeof parsed.error === "string") detail = parsed.error;
    } catch {
      // Not JSON after all — keep the raw text.
    }
  }
  if (detail.length > 200) detail = `${detail.slice(0, 200)}…`;
  return new ApiError(res.status, path, detail);
}

export async function apiFetch(server: Server, path: string, init: RequestInit = {}): Promise<Response> {
  const headers: Record<string, string> = { ...(init.headers as Record<string, string>) };
  if (server.token) headers["Authorization"] = `Bearer ${server.token}`;
  try {
    return await fetch(server.origin + path, {
      ...init,
      headers,
      credentials: server.token ? "omit" : "same-origin",
    });
  } catch {
    // fetch only rejects when the request never completed.
    throw new ApiError(0, path);
  }
}

export async function getJSON<T>(server: Server, path: string): Promise<T> {
  const res = await apiFetch(server, path);
  if (!res.ok) throw await failed(path, res);
  return (await res.json()) as T;
}

export async function postJSON<T>(server: Server, path: string, body?: unknown): Promise<T> {
  const res = await apiFetch(server, path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!res.ok) throw await failed(path, res);
  const text = await res.text();
  return (text ? JSON.parse(text) : {}) as T;
}

export async function putJSON<T>(server: Server, path: string, body?: unknown): Promise<T> {
  const res = await apiFetch(server, path, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!res.ok) throw await failed(path, res);
  const text = await res.text();
  return (text ? JSON.parse(text) : {}) as T;
}

export async function del(server: Server, path: string): Promise<void> {
  const res = await apiFetch(server, path, { method: "DELETE" });
  if (!res.ok) throw await failed(path, res);
}

export function wsURL(server: Server, path: string): string {
  const base = server.origin.replace(/^http/, "ws") + path;
  return server.token ? `${base}?token=${encodeURIComponent(server.token)}` : base;
}
