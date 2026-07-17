import { useState } from "react";
import { postJSON } from "../api";
import { localServer } from "../servers";
import { parseOpenerOrigin } from "./openerOrigin";

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
