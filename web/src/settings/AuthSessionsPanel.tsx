import { del } from "../api";
import { localServer } from "../servers";
import { useFetch } from "../useFetch";
import PanelState from "./PanelState";

type AuthSession = { tokenHash: string; userAgent: string; createdAt: string; expiresAt: string };

export default function AuthSessionsPanel() {
  const { data, error, loading, reload } = useFetch<AuthSession[]>("/api/auth/sessions");
  const sessions = data ?? [];

  async function revoke(tokenHash: string) {
    await del(localServer(), `/api/auth/sessions/${tokenHash}`);
    reload();
  }

  return (
    <section>
      <h2>Auth Sessions</h2>
      <PanelState loading={loading} error={error} onRetry={reload} />
      {!loading && !error && (
        <>
          {sessions.length === 0 && <p className="empty-note">No active sessions.</p>}
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
                  <td>{new Date(s.createdAt).toLocaleDateString()}</td>
                  <td>{new Date(s.expiresAt).toLocaleDateString()}</td>
                  <td>
                    <button className="danger" onClick={() => revoke(s.tokenHash)}>
                      revoke
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </>
      )}
    </section>
  );
}
