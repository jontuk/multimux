import { useCallback, useState } from "react";
import { errorText } from "../api";

/**
 * Runs a settings list's mutating requests (add, edit, delete) and keeps the
 * reason the last one failed. `onDone` and `reload` run only on success, so a
 * refused request leaves what the user typed in place to correct.
 */
export function useAction(reload: () => void) {
  const [actionError, setActionError] = useState("");

  const run = useCallback(
    async (request: () => Promise<unknown>, onDone?: () => void) => {
      setActionError("");
      try {
        await request();
      } catch (e: unknown) {
        setActionError(errorText(e));
        return;
      }
      onDone?.();
      reload();
    },
    [reload],
  );

  return { actionError, run };
}
