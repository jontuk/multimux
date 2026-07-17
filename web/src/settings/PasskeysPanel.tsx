import { useCallback, useEffect, useState } from "react";
import { del, getJSON } from "../api";
import { localServer } from "../servers";
import { register } from "../webauthn";

type Credential = { id: string; name: string; createdAt: string; lastUsedAt: string | null };

export default function PasskeysPanel() {
  const [credentials, setCredentials] = useState<Credential[]>([]);
  const [keyName, setKeyName] = useState("");
  const [loading, setLoading] = useState(false);

  const refresh = useCallback(() => {
    getJSON<Credential[]>(localServer(), "/api/auth/credentials")
      .then(setCredentials)
      .catch(() => setCredentials([]));
  }, []);
  useEffect(refresh, [refresh]);

  async function addPasskey() {
    if (!keyName.trim()) return;
    try {
      setLoading(true);
      await register(
        localServer(),
        "/api/auth/register/begin",
        { keyName },
        `/api/auth/register/finish?keyName=${encodeURIComponent(keyName)}`,
      );
      setKeyName("");
      refresh();
    } catch (error) {
      console.error("Failed to register passkey:", error);
    } finally {
      setLoading(false);
    }
  }

  async function revoke(credId: string) {
    if (credentials.length <= 1) {
      alert("Cannot revoke the last credential");
      return;
    }
    await del(localServer(), `/api/auth/credentials/${credId}`);
    refresh();
  }

  return (
    <section>
      <h2>Passkeys</h2>
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
      <div className="settings-form">
        <input
          placeholder="passkey name"
          value={keyName}
          onChange={(e) => setKeyName(e.target.value)}
          disabled={loading}
        />
        <button className="primary" disabled={!keyName.trim() || loading} onClick={addPasskey}>
          Add passkey
        </button>
      </div>
    </section>
  );
}
