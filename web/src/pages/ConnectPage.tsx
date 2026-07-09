import { useState } from "react";
import { postJSON } from "../api";
import { localServer } from "../servers";

export function parseOpenerOrigin(search: string): string | null {
  const raw = new URLSearchParams(search).get("opener");
  if (!raw) return null;
  try {
    const u = new URL(raw);
    const isHttps = u.protocol === "https:";
    const isDevLocalhost = u.protocol === "http:" && (u.hostname === "localhost" || u.hostname === "127.0.0.1");
    if (!isHttps && !isDevLocalhost) return null;
    return u.origin;
  } catch {
    return null;
  }
}

export default function ConnectPage() {
  // Hash routing puts the query inside the hash: #/connect?opener=...
  const query = window.location.hash.split("?")[1] ?? "";
  const openerOrigin = parseOpenerOrigin("?" + query);
  const [error, setError] = useState("");

  async function approve() {
    try {
      const { token } = await postJSON<{ token: string }>(localServer(), "/api/auth/token");
      window.opener?.postMessage({ type: "multimux-token", token }, openerOrigin!);
      window.close();
    } catch (e) {
      setError(String(e));
    }
  }

  if (!openerOrigin) return <p className="error">Invalid connect request.</p>;
  return (
    <div className="connect-page">
      <h1>Grant access?</h1>
      <p>
        <b>{openerOrigin}</b> is asking for a token to control sessions on this daemon.
      </p>
      <button onClick={approve}>Approve</button>
      <button onClick={() => window.close()}>Deny</button>
      {error && <p className="error">{error}</p>}
    </div>
  );
}
