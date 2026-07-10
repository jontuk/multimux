import { useState } from "react";
import { login } from "../webauthn";
import { localServer } from "../servers";

export default function LoginPage() {
  const [error, setError] = useState("");
  async function onLogin() {
    try {
      await login(localServer());
      if (!window.location.hash.startsWith("#/connect")) window.location.hash = "#/";
      window.location.reload();
    } catch (e) {
      setError(String(e));
    }
  }
  return (
    <div className="auth-page login-page">
      <div className="auth-wordmark">
        <span className="prompt">$</span>
        multimux
        <span className="cursor" aria-hidden="true" />
      </div>
      <div className="auth-card">
        <button onClick={onLogin}>Sign in with passkey</button>
        {error && <p className="error">{error}</p>}
      </div>
    </div>
  );
}
