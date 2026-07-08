import type { Server } from "./servers";
import { apiFetch, postJSON } from "./api";

function b64urlToBuf(s: string): ArrayBuffer {
  const pad = "=".repeat((4 - (s.length % 4)) % 4);
  const bin = atob(s.replace(/-/g, "+").replace(/_/g, "/") + pad);
  return Uint8Array.from(bin, (c) => c.charCodeAt(0)).buffer;
}

function bufToB64url(b: ArrayBuffer): string {
  return btoa(String.fromCharCode(...new Uint8Array(b)))
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "");
}

// register drives create(); beginBody goes to beginPath, attestation to finishPath.
export async function register(
  server: Server,
  beginPath: string,
  beginBody: unknown,
  finishPath: string,
): Promise<void> {
  const creation = await postJSON<{ publicKey: PublicKeyCredentialCreationOptionsJSON }>(server, beginPath, beginBody);
  const pk = creation.publicKey;
  const publicKey: PublicKeyCredentialCreationOptions = {
    ...pk,
    challenge: b64urlToBuf(pk.challenge),
    user: { ...pk.user, id: b64urlToBuf(pk.user.id) },
    excludeCredentials: (pk.excludeCredentials ?? []).map((c) => ({ ...c, id: b64urlToBuf(c.id) })),
  };
  const cred = (await navigator.credentials.create({ publicKey })) as PublicKeyCredential;
  const resp = cred.response as AuthenticatorAttestationResponse;
  const body = JSON.stringify({
    id: cred.id,
    rawId: bufToB64url(cred.rawId),
    type: cred.type,
    response: {
      attestationObject: bufToB64url(resp.attestationObject),
      clientDataJSON: bufToB64url(resp.clientDataJSON),
    },
  });
  const res = await apiFetch(server, finishPath, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body,
  });
  if (!res.ok) throw new Error(`registration failed: ${res.status}`);
}

export async function login(server: Server): Promise<void> {
  const assertion = await postJSON<{ publicKey: PublicKeyCredentialRequestOptionsJSON }>(
    server,
    "/api/auth/login/begin",
  );
  const pk = assertion.publicKey;
  const publicKey: PublicKeyCredentialRequestOptions = {
    ...pk,
    challenge: b64urlToBuf(pk.challenge),
    allowCredentials: (pk.allowCredentials ?? []).map((c) => ({ ...c, id: b64urlToBuf(c.id) })),
  };
  const cred = (await navigator.credentials.get({ publicKey })) as PublicKeyCredential;
  const resp = cred.response as AuthenticatorAssertionResponse;
  const body = JSON.stringify({
    id: cred.id,
    rawId: bufToB64url(cred.rawId),
    type: cred.type,
    response: {
      authenticatorData: bufToB64url(resp.authenticatorData),
      clientDataJSON: bufToB64url(resp.clientDataJSON),
      signature: bufToB64url(resp.signature),
      userHandle: resp.userHandle ? bufToB64url(resp.userHandle) : null,
    },
  });
  const res = await apiFetch(server, "/api/auth/login/finish", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body,
  });
  if (!res.ok) throw new Error(`login failed: ${res.status}`);
}

// Shapes returned by go-webauthn as JSON (base64url-encoded byte fields).
type PublicKeyCredentialCreationOptionsJSON = Omit<
  PublicKeyCredentialCreationOptions,
  "challenge" | "user" | "excludeCredentials"
> & {
  challenge: string;
  user: Omit<PublicKeyCredentialUserEntity, "id"> & { id: string };
  excludeCredentials?: (Omit<PublicKeyCredentialDescriptor, "id"> & { id: string })[];
};

type PublicKeyCredentialRequestOptionsJSON = Omit<
  PublicKeyCredentialRequestOptions,
  "challenge" | "allowCredentials"
> & {
  challenge: string;
  allowCredentials?: (Omit<PublicKeyCredentialDescriptor, "id"> & { id: string })[];
};
