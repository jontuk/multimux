import { useState } from "react";
import { addServer, connectServer, InvalidOriginError, invalidServers, listServers, removeServer } from "../servers";

// Stored entries nothing can connect to are listed too, marked, so they can be
// removed here; the grid never sees them (see invalidServers).
function readServers() {
  return { usable: listServers(), invalid: invalidServers() };
}

export default function ServersPanel() {
  const [servers, setServers] = useState(readServers);
  const [origin, setOrigin] = useState("");
  const [name, setName] = useState("");
  const [addError, setAddError] = useState("");

  function add() {
    if (!origin.trim() || !name.trim()) return;
    try {
      addServer(origin, name.trim());
    } catch (err) {
      if (!(err instanceof InvalidOriginError)) throw err;
      setAddError(err.message);
      return;
    }
    setAddError("");
    setOrigin("");
    setName("");
    setServers(readServers());
  }

  function remove(id: string) {
    removeServer(id);
    setServers(readServers());
  }

  return (
    <section>
      <h2>Servers</h2>
      {addError && <div className="server-status-banner">{addError}</div>}
      <table>
        <thead>
          <tr>
            <th>Name</th>
            <th>Origin</th>
            <th>Action</th>
          </tr>
        </thead>
        <tbody>
          {servers.usable.map((s) => (
            <tr key={s.id}>
              <td>{s.name}</td>
              <td>{s.origin}</td>
              <td>
                <button className="danger" disabled={s.id === "local"} onClick={() => remove(s.id)}>
                  remove
                </button>
                {s.id !== "local" && (
                  <button className="primary" onClick={() => connectServer(s)}>
                    Connect
                  </button>
                )}
              </td>
            </tr>
          ))}
          {servers.invalid.map((s, i) => (
            // Hand-edited storage can repeat an id; the index keeps keys unique.
            <tr key={`invalid-${i}`}>
              <td>{s.name}</td>
              <td>
                {s.origin} <span className="server-invalid">— invalid address, not used</span>
              </td>
              <td>
                <button className="danger" onClick={() => remove(s.id)}>
                  remove
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      <div className="settings-form">
        <input
          placeholder="origin"
          value={origin}
          onChange={(e) => {
            setOrigin(e.target.value);
            setAddError("");
          }}
        />
        <input placeholder="name" value={name} onChange={(e) => setName(e.target.value)} />
        <button className="primary" disabled={!origin.trim() || !name.trim()} onClick={add}>
          Add server
        </button>
      </div>
    </section>
  );
}
