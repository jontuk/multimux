import { describe, expect, test } from "vitest";
import { endedTileKeys } from "../grid/endedSessions";
import type { Layout } from "../grid/model";
import type { Session } from "../grid/types";

const layout: Layout = {
  shape: { rows: 2, cols: 2 },
  tiles: [
    { serverId: "local", sessionId: 1 },
    { serverId: "local", sessionId: 2 },
    { serverId: "remote", sessionId: 3 },
    { serverId: "local", sessionId: 99 },
  ],
};

const sessions: Session[] = [
  { id: 1, tmuxName: "mm-1", toolId: 1, dir: "/a", status: "running" },
  { id: 2, tmuxName: "mm-2", toolId: 1, dir: "/b", status: "dead" },
  { id: 3, tmuxName: "mm-3", toolId: 1, dir: "/c", status: "dead" },
];

describe("endedTileKeys", () => {
  test("returns dead and missing tiles for the requested server", () => {
    expect([...endedTileKeys(layout, "local", sessions)]).toEqual(["local:2", "local:99"]);
  });

  test("does not include another server's tiles", () => {
    expect([...endedTileKeys(layout, "remote", sessions)]).toEqual(["remote:3"]);
  });

  test("returns no keys when every placed session is running", () => {
    const running = sessions.map((session) => ({ ...session, status: "running" }));
    expect([...endedTileKeys(layout, "local", [...running, { ...running[0], id: 99 }])]).toEqual([]);
  });
});
