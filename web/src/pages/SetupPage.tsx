import { useState } from "react";
import { register } from "../webauthn";
import { localServer } from "../servers";

export default function SetupPage() {
  const code = new URLSearchParams(window.location.search).get("code") ?? "";
  const [userName, setUserName] = useState("");
  const [keyName, setKeyName] = useState("");
  const [error, setError] = useState("");
  async function onSetup() {
    try {
      await register(
        localServer(),
        "/api/auth/setup/begin",
        { code, userName, keyName },
        `/api/auth/setup/finish?code=${encodeURIComponent(code)}&keyName=${encodeURIComponent(keyName)}`,
      );
      window.location.href = "/";
    } catch (e) {
      setError(String(e));
    }
  }
  return (
    <div className="setup-page">
      <h1>multimux first-run setup</h1>
      {!code && <p className="error">Missing setup code — use the URL printed by the daemon.</p>}
      <label>
        Your name <input value={userName} onChange={(e) => setUserName(e.target.value)} />
      </label>
      <label>
        Passkey name <input value={keyName} placeholder="laptop" onChange={(e) => setKeyName(e.target.value)} />
      </label>
      <button disabled={!code || !userName || !keyName} onClick={onSetup}>
        Register passkey
      </button>
      {error && <p className="error">{error}</p>}
    </div>
  );
}
