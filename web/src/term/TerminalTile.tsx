import type { Server } from "../servers";

export default function TerminalTile({
  server,
  sessionId,
}: {
  server: Server;
  sessionId: number;
  onClose: () => void;
}) {
  return (
    <div className="terminal-tile">
      terminal (Task 22): {server.name}#{sessionId}
    </div>
  );
}
