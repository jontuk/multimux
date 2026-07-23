import { useState } from "react";
import { del, errorText } from "../api";
import { localServer } from "../servers";
import { register } from "../webauthn";
import { useFetch } from "../useFetch";
import PanelState from "./PanelState";

type Credential = { id: string; name: string; createdAt: string; lastUsedAt: string | null };

export default function PasskeysPanel() {
  const { data, error, loading, reload } = useFetch<Credential[]>("/api/auth/credentials");
  const [keyName, setKeyName] = useState("");
  const [busy, setBusy] = useState(false);
  const [addError, setAddError] = useState("");
  const credentials = data ?? [];

  async function addPasskey() {
    if (!keyName.trim()) return;
    setAddError("");
    try {
      setBusy(true);
      await register(
        localServer(),
        "/api/auth/register/begin",
        { keyName },
        `/api/auth/register/finish?keyName=${encodeURIComponent(keyName)}`,
      );
      setKeyName("");
      reload();
    } catch (err) {
      setAddError(errorText(err));
    } finally {
      setBusy(false);
    }
  }

  async function revoke(credId: string) {
    if (credentials.length <= 1) {
      alert("Cannot revoke the last credential");
      return;
    }
    await del(localServer(), `/api/auth/credentials/${credId}`);
    reload();
  }

  return (
    <section>
      <h2>Passkeys</h2>
      <PanelState loading={loading} error={error} onRetry={reload} />
      {addError && <div className="server-status-banner">{addError}</div>}
      {!loading && !error && (
        <>
          {credentials.length === 0 && <p className="empty-note">No passkeys registered yet.</p>}
          <table>
            <thead>
              <tr>
                <th>Name</th>
                <th>Created</th>
                <th>Last Used</th>
                <th>Action</th>
              </tr>
            </thead>
            <tbody>
              {credentials.map((c) => (
                <tr key={c.id}>
                  <td>{c.name}</td>
                  <td>{new Date(c.createdAt).toLocaleDateString()}</td>
                  <td>{c.lastUsedAt ? new Date(c.lastUsedAt).toLocaleDateString() : "Never"}</td>
                  <td>
                    <button className="danger" disabled={credentials.length <= 1} onClick={() => revoke(c.id)}>
                      revoke
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </>
      )}
      <div className="settings-form">
        <input
          placeholder="passkey name"
          value={keyName}
          onChange={(e) => setKeyName(e.target.value)}
          disabled={busy}
        />
        <button className="primary" disabled={!keyName.trim() || busy} onClick={addPasskey}>
          Add passkey
        </button>
      </div>
    </section>
  );
}
