import type { Server } from "../servers";
import type { Layout } from "./model";
import type { Session } from "./types";

export type MobileSession = {
  key: string;
  server: Server;
  session: Session;
};

export type MobileSelection = { key: string | null; index: number };

export function orderMobileSessions(
  layout: Layout,
  servers: Server[],
  sessionsByServer: Record<string, Session[]>,
): MobileSession[] {
  const serverById = new Map(servers.map((server) => [server.id, server]));
  const sessionByKey = new Map<string, MobileSession>();
  for (const server of servers) {
    for (const session of sessionsByServer[server.id] ?? []) {
      if (session.status === "running") {
        const key = `${server.id}:${session.id}`;
        sessionByKey.set(key, { key, server, session });
      }
    }
  }

  const seen = new Set<string>();
  const result: MobileSession[] = [];
  for (const tile of layout.tiles) {
    if (!tile || !serverById.has(tile.serverId)) continue;
    const key = `${tile.serverId}:${tile.sessionId}`;
    const entry = sessionByKey.get(key);
    if (entry && !seen.has(key)) {
      seen.add(key);
      result.push(entry);
    }
  }

  for (const server of servers) {
    for (const session of sessionsByServer[server.id] ?? []) {
      const key = `${server.id}:${session.id}`;
      const entry = sessionByKey.get(key);
      if (entry && !seen.has(key)) {
        seen.add(key);
        result.push(entry);
      }
    }
  }
  return result;
}

export function reconcileMobileSelection(previous: MobileSelection, sessions: MobileSession[]): MobileSelection {
  if (sessions.length === 0) return { key: null, index: 0 };
  const retained = sessions.findIndex((session) => session.key === previous.key);
  const index = retained >= 0 ? retained : Math.min(previous.index, sessions.length - 1);
  return { key: sessions[index].key, index };
}
