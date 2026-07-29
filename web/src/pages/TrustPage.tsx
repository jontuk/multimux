import { useFetch } from "../useFetch";

type CAInfo = {
  subject: string;
  permittedDNSDomains: string[];
  expires: string;
  sha256Fingerprint: string;
};

// Exported beside the page so the navigation security boundary stays directly testable.
// eslint-disable-next-line react-refresh/only-export-components
export function safeReturnTarget(raw: string | null, origin = window.location.origin): string {
  if (!raw || raw.trimStart().startsWith("//")) return "/";
  try {
    const resolved = new URL(raw, origin);
    if (resolved.origin !== origin || resolved.pathname.startsWith("//")) return "/";
    return `${resolved.pathname}${resolved.search}${resolved.hash}`;
  } catch {
    return "/";
  }
}

function formatExpiry(raw: string): string {
  const formatted = new Intl.DateTimeFormat("en-GB", {
    dateStyle: "long",
    timeStyle: "short",
    timeZone: "UTC",
  }).format(new Date(raw));
  return `${formatted} UTC`;
}

export default function TrustPage() {
  const { data: info, error, loading, reload } = useFetch<CAInfo>("/ca/info");
  const returnTarget = safeReturnTarget(new URLSearchParams(window.location.search).get("return"));

  return (
    <main className="auth-page trust-page">
      <div className="auth-wordmark">
        <span className="prompt">$</span>
        multimux
        <span className="cursor" aria-hidden="true" />
      </div>
      <article className="auth-card">
        <p className="eyebrow">Android trust</p>
        <h1>Trust multimux on Android</h1>
        <p>Android must trust this daemon's local CA before Chrome can use passkeys or install the PWA.</p>

        {loading && <p>Loading certificate information…</p>}
        {error && (
          <section>
            <p className="error">Could not load certificate information: {error}</p>
            <button onClick={reload}>Retry</button>
          </section>
        )}
        {info && (
          <section aria-label="Certificate information">
            <h2>Certificate information</h2>
            <dl>
              <dt>Subject</dt>
              <dd>{info.subject}</dd>
              <dt>Constrained hostnames</dt>
              <dd>
                <ul>
                  {info.permittedDNSDomains.map((hostname) => (
                    <li key={hostname}>{hostname}</li>
                  ))}
                </ul>
              </dd>
              <dt>Expires</dt>
              <dd>{formatExpiry(info.expires)}</dd>
              <dt>SHA-256 fingerprint</dt>
              <dd>{info.sha256Fingerprint}</dd>
            </dl>
            <a href="/ca.crt" download="multimux-ca.crt">
              Download CA certificate
            </a>
          </section>
        )}

        <section>
          <h2>Install on Pixel or stock Android</h2>
          <ol>
            <li>Download multimux-ca.crt.</li>
            <li>
              Open Settings → Security &amp; privacy → More security settings → Encryption &amp; credentials → Install a
              certificate → CA certificate.
            </li>
            <li>Select the downloaded file and approve Android's CA warning.</li>
            <li>Return to Chrome, then reload this page.</li>
          </ol>
          <p>
            On Samsung and some other devices, wording differs. Use Security and privacy → More security settings →
            Install from device storage.
          </p>
        </section>

        <section>
          <h2>Verify before continuing</h2>
          <p>Use this guided download only on a LAN, VPN, or tailnet you control.</p>
          <p>
            Compare the installed certificate's SHA-256 fingerprint with the value printed by the daemon, not merely
            with this initially untrusted web page. Do not continue if they do not match.
          </p>
          <p>
            The certificate's DNS name constraints limit it to the configured daemon names, but do not authenticate the
            initial download.
          </p>
          <p>
            If this is a managed device, policy may block user-added CAs. Multimux cannot bypass device management or
            weaken certificate validation.
          </p>
          <p>
            If you do not fully control the network, transfer pki/ca.pem over an already trusted channel such as SSH to
            a computer, then use USB or Quick Share to move it to the phone.
          </p>
        </section>

        {window.isSecureContext ? (
          <section className="trust-success">
            <h2>Android now trusts this multimux daemon</h2>
            <a className="primary" href={returnTarget}>
              Continue
            </a>
          </section>
        ) : (
          <button onClick={() => window.location.reload()}>Reload and check trust</button>
        )}
      </article>
    </main>
  );
}
