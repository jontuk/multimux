import { useState } from "react";
import { addServer, listServers, removeServer } from "../servers";

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
                {s.id !== "local" && <button>Connect</button>}
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
