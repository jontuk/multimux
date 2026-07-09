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
    <div className="login-page">
      <h1>multimux</h1>
      <button onClick={onLogin}>Sign in with passkey</button>
      {error && <p className="error">{error}</p>}
    </div>
  );
}
