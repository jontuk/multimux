import { addServer, listServers, localServer, removeServer, setServerToken } from "../servers";

beforeEach(() => localStorage.clear());

test("local server always first", () => {
  const list = listServers();
  expect(list[0].id).toBe("local");
  expect(list[0].origin).toBe(window.location.origin);
});

test("add, token, remove round-trip", () => {
  const s = addServer("https://otherbox:8686", "otherbox");
  setServerToken(s.id, "tok123");
  const found = listServers().find((x) => x.id === s.id);
  expect(found?.token).toBe("tok123");
  removeServer(s.id);
  expect(listServers().find((x) => x.id === s.id)).toBeUndefined();
});

test("localServer returns fresh object equal to itself", () => {
  expect(localServer()).toEqual(localServer());
});
