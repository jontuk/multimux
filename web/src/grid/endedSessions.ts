import { tileKey, type Layout } from "./model";
import type { Session } from "./types";

/**
 * Classifies placed tiles against one successful, authoritative session list.
 * The caller owns loading and connection-status gates; absence is meaningful
 * only after that gate has been passed.
 */
export function endedTileKeys(layout: Layout, serverId: string, sessions: Session[]): Set<string> {
  const byId = new Map(sessions.map((session) => [session.id, session]));
  const ended = new Set<string>();
  for (const tile of layout.tiles) {
    if (!tile || tile.serverId !== serverId) continue;
    const session = byId.get(tile.sessionId);
    if (!session || session.status !== "running") ended.add(tileKey(tile));
  }
  return ended;
}
