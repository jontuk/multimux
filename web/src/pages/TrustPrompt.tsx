// Exported beside the prompt so its return-path encoding stays directly testable.
// eslint-disable-next-line react-refresh/only-export-components
export function currentReturnTarget(): string {
  return `${window.location.pathname}${window.location.search}${window.location.hash}`;
}

export default function TrustPrompt() {
  const href = `/trust?return=${encodeURIComponent(currentReturnTarget())}`;
  return (
    <div className="auth-card trust-prompt">
      <p>Android must trust this daemon's local CA before passkeys and the installed app can work.</p>
      <a className="primary" href={href}>
        Install the Android CA
      </a>
    </div>
  );
}
