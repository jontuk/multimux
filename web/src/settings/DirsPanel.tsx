import { useCallback, useEffect, useState } from "react";
import { del, getJSON, postJSON } from "../api";
import { localServer } from "../servers";

type Dir = { id: number; name: string; path: string };

export default function DirsPanel() {
  const [dirs, setDirs] = useState<Dir[]>([]);
  const [name, setName] = useState("");
  const [path, setPath] = useState("");

  const refresh = useCallback(() => {
    getJSON<Dir[]>(localServer(), "/api/dirs")
      .then(setDirs)
      .catch(() => setDirs([]));
  }, []);
  useEffect(refresh, [refresh]);

  async function add() {
    await postJSON(localServer(), "/api/dirs", { name, path });
    setName("");
    setPath("");
    refresh();
  }

  return (
    <section>
      <h2>Directories</h2>
      {dirs.length === 0 && <p className="empty-note">No directories yet. Add one below.</p>}
      <table>
        <tbody>
          {dirs.map((d) => (
            <tr key={d.id}>
              <td>{d.name}</td>
              <td>
                <code>{d.path}</code>
              </td>
              <td>
                <button className="danger" onClick={() => del(localServer(), `/api/dirs/${d.id}`).then(refresh)}>
                  delete
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      <div className="settings-form">
        <input placeholder="name" value={name} onChange={(e) => setName(e.target.value)} />
        <input placeholder="path" value={path} onChange={(e) => setPath(e.target.value)} />
        <button className="primary" disabled={!name || !path} onClick={add}>
          Add dir
        </button>
      </div>
    </section>
  );
}
