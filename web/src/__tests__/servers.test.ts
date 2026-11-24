import {
  addServer,
  InvalidOriginError,
  invalidServers,
  listServers,
  localServer,
  removeServer,
  setServerToken,
} from "../servers";

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

test("addServer stores the bare origin", () => {
  expect(addServer("https://otherbox:8686/", "a").origin).toBe("https://otherbox:8686");
  expect(addServer("  https://OtherBox:8686/some/path?x=1#/grid ", "b").origin).toBe("https://otherbox:8686");
  expect(addServer("http://10.0.0.5:8686", "c").origin).toBe("http://10.0.0.5:8686");
});

test("addServer assumes https when no scheme is given", () => {
  // new URL("mux.local:8686") parses, with "mux.local:" as the scheme.
  expect(addServer("mux.local:8686", "a").origin).toBe("https://mux.local:8686");
  expect(addServer("otherbox", "b").origin).toBe("https://otherbox");
  expect(addServer("localhost:8686/", "c").origin).toBe("https://localhost:8686");
});

test.each([
  "",
  "   ",
  "ftp://otherbox",
  "ws://otherbox:8686",
  "javascript:alert(1)",
  "mailto:me@otherbox",
  "https://",
  "not a host",
])("addServer rejects %j and stores nothing", (origin) => {
  expect(() => addServer(origin, "bad")).toThrow(InvalidOriginError);
  expect(localStorage.getItem("multimux.servers")).toBeNull();
});

test.each(["{not json", '"a string"', "null", "42", '{"id":"x","origin":"https://otherbox","name":"n"}'])(
  "stored list %j reads as empty",
  (raw) => {
    localStorage.setItem("multimux.servers", raw);
    expect(listServers()).toEqual([localServer()]);
    expect(invalidServers()).toEqual([]);
  },
);

test("entries missing fields are ignored", () => {
  const good = { id: "g", origin: "https://otherbox:8686", name: "good", token: "tok" };
  localStorage.setItem(
    "multimux.servers",
    JSON.stringify([
      null,
      7,
      "x",
      {},
      { id: "a", origin: "https://a" },
      { id: 1, origin: "https://a", name: "n" },
      good,
    ]),
  );
  expect(listServers()).toEqual([localServer(), good]);
  expect(invalidServers()).toEqual([]);
});

test("an entry with an unusable origin is kept for removal but never listed", () => {
  const good = { id: "g", origin: "https://otherbox:8686", name: "good" };
  const noScheme = { id: "b1", origin: "mux.local:8686", name: "typo" };
  const wrongScheme = { id: "b2", origin: "ftp://otherbox", name: "ftp" };
  localStorage.setItem("multimux.servers", JSON.stringify([noScheme, good, wrongScheme]));

  expect(listServers()).toEqual([localServer(), good]);
  expect(invalidServers()).toEqual([noScheme, wrongScheme]);

  // Writes for other servers leave it in place rather than silently dropping it.
  setServerToken("g", "tok");
  expect(invalidServers()).toEqual([noScheme, wrongScheme]);

  removeServer("b1");
  removeServer("b2");
  expect(invalidServers()).toEqual([]);
  expect(listServers()).toEqual([localServer(), { ...good, token: "tok" }]);
});

test("a stored origin with a path or trailing slash is listed as its bare origin", () => {
  localStorage.setItem(
    "multimux.servers",
    JSON.stringify([{ id: "g", origin: "https://otherbox:8686/x/", name: "n" }]),
  );
  expect(listServers()[1].origin).toBe("https://otherbox:8686");
});

test("a non-string token is not handed out", () => {
  localStorage.setItem(
    "multimux.servers",
    JSON.stringify([{ id: "g", origin: "https://otherbox", name: "n", token: 5 }]),
  );
  expect(listServers()[1]).toEqual({ id: "g", origin: "https://otherbox", name: "n" });
});
