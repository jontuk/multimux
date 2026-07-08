import { useCallback, useEffect, useState } from "react";
import { del, getJSON, postJSON } from "../api";
import { localServer } from "../servers";

type Tool = { id: number; name: string; command: string };

export default function ToolsPanel() {
  const [tools, setTools] = useState<Tool[]>([]);
  const [name, setName] = useState("");
  const [command, setCommand] = useState("");

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

  return (
    <section>
      <h2>Tools</h2>
      <table>
        <tbody>
          {tools.map((t) => (
            <tr key={t.id}>
              <td>{t.name}</td>
              <td>
                <code>{t.command}</code>
              </td>
              <td>
                <button onClick={() => del(localServer(), `/api/tools/${t.id}`).then(refresh)}>delete</button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      <input placeholder="name" value={name} onChange={(e) => setName(e.target.value)} />
      <input placeholder="command" value={command} onChange={(e) => setCommand(e.target.value)} />
      <button disabled={!name || !command} onClick={add}>
        Add tool
      </button>
    </section>
  );
}
