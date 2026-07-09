import { useState } from "react";
import { addServer, listServers, removeServer, setServerToken, type Server } from "../servers";

function connectServer(server: Server) {
  const popup = window.open(`${server.origin}/#/connect?opener=${encodeURIComponent(window.location.origin)}`);
  function onMsg(ev: MessageEvent) {
    if (ev.origin !== server.origin || ev.data?.type !== "multimux-token") return;
    setServerToken(server.id, ev.data.token);
    window.removeEventListener("message", onMsg);
    popup?.close();
  }
  window.addEventListener("message", onMsg);
}

export default function ServersPanel() {
  const [servers, setServers] = useState(() => listServers());
  const [origin, setOrigin] = useState("");
  const [name, setName] = useState("");

  function add() {
    if (!origin.trim() || !name.trim()) return;
    addServer(origin, name);
    setOrigin("");
    setName("");
    setServers(listServers());
  }

  function remove(id: string) {
    removeServer(id);
    setServers(listServers());
  }

  return (
    <section>
      <h2>Servers</h2>
      <table>
        <thead>
          <tr>
            <th>Name</th>
            <th>Origin</th>
            <th>Action</th>
          </tr>
        </thead>
        <tbody>
          {servers.map((s) => (
            <tr key={s.id}>
              <td>{s.name}</td>
              <td>{s.origin}</td>
              <td>
                <button disabled={s.id === "local"} onClick={() => remove(s.id)}>
                  remove
                </button>
                {s.id !== "local" && <button onClick={() => connectServer(s)}>Connect</button>}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      <input placeholder="origin" value={origin} onChange={(e) => setOrigin(e.target.value)} />
      <input placeholder="name" value={name} onChange={(e) => setName(e.target.value)} />
      <button disabled={!origin.trim() || !name.trim()} onClick={add}>
        Add server
      </button>
    </section>
  );
}
