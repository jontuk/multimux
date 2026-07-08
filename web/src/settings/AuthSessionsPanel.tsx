import { useCallback, useEffect, useState } from "react";
import { del, getJSON } from "../api";
import { localServer } from "../servers";

type AuthSession = { tokenHash: string; userAgent: string; created: string; expires: string };

export default function AuthSessionsPanel() {
  const [sessions, setSessions] = useState<AuthSession[]>([]);

  const refresh = useCallback(() => {
    getJSON<AuthSession[]>(localServer(), "/api/auth/sessions")
      .then(setSessions)
      .catch(() => setSessions([]));
  }, []);
  useEffect(refresh, [refresh]);

  async function revoke(tokenHash: string) {
    await del(localServer(), `/api/auth/sessions/${tokenHash}`);
    refresh();
  }

  return (
    <section>
      <h2>Auth Sessions</h2>
      <table>
        <thead>
          <tr>
            <th>User Agent</th>
            <th>Created</th>
            <th>Expires</th>
            <th>Action</th>
          </tr>
        </thead>
        <tbody>
          {sessions.map((s) => (
            <tr key={s.tokenHash}>
              <td>{s.userAgent}</td>
              <td>{new Date(s.created).toLocaleDateString()}</td>
              <td>{new Date(s.expires).toLocaleDateString()}</td>
              <td>
                <button onClick={() => revoke(s.tokenHash)}>revoke</button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}
