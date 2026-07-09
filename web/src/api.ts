import type { Server } from "./servers";

export async function apiFetch(server: Server, path: string, init: RequestInit = {}): Promise<Response> {
  const headers: Record<string, string> = { ...(init.headers as Record<string, string>) };
  if (server.token) headers["Authorization"] = `Bearer ${server.token}`;
  return fetch(server.origin + path, {
    ...init,
    headers,
    credentials: server.token ? "omit" : "same-origin",
  });
}

export async function getJSON<T>(server: Server, path: string): Promise<T> {
  const res = await apiFetch(server, path);
  if (!res.ok) throw new Error(`${path}: ${res.status}`);
  return (await res.json()) as T;
}

export async function postJSON<T>(server: Server, path: string, body?: unknown): Promise<T> {
  const res = await apiFetch(server, path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!res.ok) throw new Error(`${path}: ${res.status}`);
  const text = await res.text();
  return (text ? JSON.parse(text) : {}) as T;
}

export async function putJSON<T>(server: Server, path: string, body?: unknown): Promise<T> {
  const res = await apiFetch(server, path, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!res.ok) throw new Error(`${path}: ${res.status}`);
  const text = await res.text();
  return (text ? JSON.parse(text) : {}) as T;
}

export async function del(server: Server, path: string): Promise<void> {
  const res = await apiFetch(server, path, { method: "DELETE" });
  if (!res.ok) throw new Error(`${path}: ${res.status}`);
}

export function wsURL(server: Server, path: string): string {
  const base = server.origin.replace(/^http/, "ws") + path;
  return server.token ? `${base}?token=${encodeURIComponent(server.token)}` : base;
}
