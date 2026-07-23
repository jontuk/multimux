/**
 * The one loading/error vocabulary every settings panel uses. Renders nothing
 * once the data has arrived, so panels can show their real content instead.
 */
export default function PanelState({
  loading,
  error,
  onRetry,
}: {
  loading: boolean;
  error: string | null;
  onRetry: () => void;
}) {
  if (loading) return <p className="panel-state">Loading…</p>;
  if (error)
    return (
      <p className="panel-state panel-state-error">
        <span>{error}</span>
        <button onClick={onRetry}>Retry</button>
      </p>
    );
  return null;
}
