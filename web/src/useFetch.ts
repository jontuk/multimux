import { useCallback, useEffect, useState } from "react";
import { errorText, getJSON } from "./api";
import { localServer } from "./servers";

type State<T> = { data: T | null; error: string | null; loading: boolean };

/**
 * GET `path` from the local daemon, keeping loading / error / data apart so a
 * failed request never renders as "you have nothing configured". `onData` (if
 * given, memoised) runs on every successful load, for panels that seed form
 * fields from the response.
 */
export function useFetch<T>(path: string, onData?: (data: T) => void): State<T> & { reload: () => void } {
  const [state, setState] = useState<State<T>>({ data: null, error: null, loading: true });
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let live = true;
    getJSON<T>(localServer(), path)
      .then((data) => {
        if (!live) return;
        setState({ data, error: null, loading: false });
        onData?.(data);
      })
      .catch((e: unknown) => {
        if (!live) return;
        setState({ data: null, error: errorText(e), loading: false });
      });
    return () => {
      live = false;
    };
  }, [path, attempt, onData]);

  const reload = useCallback(() => {
    setState((s) => ({ ...s, error: null, loading: true }));
    setAttempt((n) => n + 1);
  }, []);

  return { ...state, reload };
}
