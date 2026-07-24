import { useState } from "react";
import { del, postJSON } from "../api";
import { localServer } from "../servers";
import { useFetch } from "../useFetch";
import PanelState from "./PanelState";
import { useReorder } from "./useReorder";

type Dir = { id: number; name: string; path: string };

export default function DirsPanel() {
  const { data, error, loading, reload } = useFetch<Dir[]>("/api/dirs");
  const [name, setName] = useState("");
  const [path, setPath] = useState("");
  const { ordered: dirs, handleProps, reorderError } = useReorder(data ?? [], "/api/dirs/order", reload);

  async function add() {
    await postJSON(localServer(), "/api/dirs", { name, path });
    setName("");
    setPath("");
    reload();
  }

  return (
    <section>
      <h2>Directories</h2>
      <PanelState loading={loading} error={error} onRetry={reload} />
      {!loading && !error && (
        <>
          {dirs.length === 0 && <p className="empty-note">No directories yet. Add one below.</p>}
          {dirs.length > 1 && (
            <p className="reorder-note">Drag the handle to reorder, or focus it and press the arrow keys.</p>
          )}
          {reorderError && <p className="reorder-error">{reorderError}</p>}
          <table>
            <tbody>
              {dirs.map((d, i) => (
                <tr key={d.id}>
                  <td {...handleProps(i, d.name)}>⠿</td>
                  <td>{d.name}</td>
                  <td>
                    <code>{d.path}</code>
                  </td>
                  <td>
                    <button className="danger" onClick={() => del(localServer(), `/api/dirs/${d.id}`).then(reload)}>
                      delete
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </>
      )}
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
