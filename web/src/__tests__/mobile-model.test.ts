import { orderMobileSessions, reconcileMobileSelection } from "../grid/mobileModel";
import type { Layout } from "../grid/model";
import type { Session } from "../grid/types";
import type { Server } from "../servers";

const local: Server = {
  id: "local",
  origin: "https://local.test",
  name: "local",
};
const remote: Server = {
  id: "remote",
  origin: "https://remote.test",
  name: "remote",
};

const session = (id: number, status = "running"): Session => ({
  id,
  tmuxName: `mm-${id}`,
  toolId: 1,
  dir: `/work/${id}`,
  status,
});

test("orders placed running sessions first, then groups remaining sessions by configured server", () => {
  const layout: Layout = {
    shape: { rows: 4, cols: 2 },
    tiles: [
      { serverId: "remote", sessionId: 9 },
      { serverId: "remote", sessionId: 9 },
      { serverId: "removed", sessionId: 7 },
      { serverId: "local", sessionId: 2 },
      { serverId: "local", sessionId: 3 },
      { serverId: "remote", sessionId: 404 },
      null,
      null,
    ],
  };
  const byServer = {
    local: [session(1), session(2), session(3, "dead")],
    remote: [session(8), session(9)],
    removed: [session(7)],
  };

  expect(orderMobileSessions(layout, [local, remote], byServer).map((entry) => entry.key)).toEqual([
    "remote:9",
    "local:2",
    "local:1",
    "remote:8",
  ]);
});

test("selects the first session when data first arrives", () => {
  const sessions = orderMobileSessions({ shape: { rows: 1, cols: 1 }, tiles: [null] }, [local], {
    local: [session(1), session(2)],
  });

  expect(reconcileMobileSelection({ key: null, index: 0 }, sessions)).toEqual({
    key: "local:1",
    index: 0,
  });
});

test("retains the selected session and updates its index", () => {
  const sessions = orderMobileSessions(
    {
      shape: { rows: 1, cols: 2 },
      tiles: [
        { serverId: "local", sessionId: 2 },
        { serverId: "local", sessionId: 1 },
      ],
    },
    [local],
    { local: [session(1), session(2)] },
  );

  expect(reconcileMobileSelection({ key: "local:1", index: 0 }, sessions)).toEqual({
    key: "local:1",
    index: 1,
  });
});

test("selects the next session at the same index when a middle selection is removed", () => {
  const withoutTwo = orderMobileSessions({ shape: { rows: 1, cols: 1 }, tiles: [null] }, [local, remote], {
    local: [session(1)],
    remote: [session(8)],
  });

  expect(reconcileMobileSelection({ key: "local:2", index: 1 }, withoutTwo)).toEqual({
    key: "remote:8",
    index: 1,
  });
});

test("selects the previous session when the last selection is removed", () => {
  const withoutLast = orderMobileSessions({ shape: { rows: 1, cols: 1 }, tiles: [null] }, [local], {
    local: [session(1), session(2)],
  });

  expect(reconcileMobileSelection({ key: "local:3", index: 2 }, withoutLast)).toEqual({
    key: "local:2",
    index: 1,
  });
});

test("clears selection when there are no sessions", () => {
  expect(reconcileMobileSelection({ key: "local:1", index: 0 }, [])).toEqual({
    key: null,
    index: 0,
  });
});
