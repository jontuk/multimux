import { useCallback, useEffect, useState } from "react";
import { del, getJSON, postJSON, putJSON } from "../api";
import { localServer } from "../servers";

type Tool = { id: number; name: string; command: string };

export default function ToolsPanel() {
  const [tools, setTools] = useState<Tool[]>([]);
  const [name, setName] = useState("");
  const [command, setCommand] = useState("");
  const [editId, setEditId] = useState<number | null>(null);
  const [editName, setEditName] = useState("");
  const [editCommand, setEditCommand] = useState("");

  const refresh = useCallback(() => {
    getJSON<Tool[]>(localServer(), "/api/tools")
      .then(setTools)
      .catch(() => setTools([]));
  }, []);
  useEffect(refresh, [refresh]);

  async function add() {
    await postJSON(localServer(), "/api/tools", { name, command });
    setName("");
    setCommand("");
    refresh();
  }

  function startEdit(t: Tool) {
    setEditId(t.id);
    setEditName(t.name);
    setEditCommand(t.command);
  }

  function cancelEdit() {
    setEditId(null);
    setEditName("");
    setEditCommand("");
  }

  async function saveEdit(id: number) {
    await putJSON(localServer(), `/api/tools/${id}`, { name: editName, command: editCommand });
    cancelEdit();
    refresh();
  }

  return (
    <section>
      <h2>Tools</h2>
      {tools.length === 0 && <p className="empty-note">No tools yet. Add one below.</p>}
      <table>
        <tbody>
          {tools.map((t) =>
            editId === t.id ? (
              <tr key={t.id}>
                <td>
                  <input aria-label="edit name" value={editName} onChange={(e) => setEditName(e.target.value)} />
                </td>
                <td>
                  <input
                    aria-label="edit command"
                    value={editCommand}
                    onChange={(e) => setEditCommand(e.target.value)}
                  />
                </td>
                <td>
                  <button className="primary" disabled={!editName || !editCommand} onClick={() => saveEdit(t.id)}>
                    save
                  </button>
                  <button onClick={cancelEdit}>cancel</button>
                </td>
              </tr>
            ) : (
              <tr key={t.id}>
                <td>{t.name}</td>
                <td>
                  <code>{t.command}</code>
                </td>
                <td>
                  <button onClick={() => startEdit(t)}>edit</button>
                  <button className="danger" onClick={() => del(localServer(), `/api/tools/${t.id}`).then(refresh)}>
                    delete
                  </button>
                </td>
              </tr>
            ),
          )}
        </tbody>
      </table>
      <div className="settings-form">
        <input placeholder="name" value={name} onChange={(e) => setName(e.target.value)} />
        <input placeholder="command" value={command} onChange={(e) => setCommand(e.target.value)} />
        <button className="primary" disabled={!name || !command} onClick={add}>
          Add tool
        </button>
      </div>
    </section>
  );
}
