import { useCallback, useEffect, useRef, useState } from "react";

/** Load data with reload support; standard shape for admin sections. */
export function useLoad<T>(fn: () => Promise<T>, deps: unknown[] = []) {
  const [data, setData] = useState<T | undefined>(undefined);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(true);
  const fnRef = useRef(fn);
  const mountedRef = useRef(false);
  const reqRef = useRef(0);
  fnRef.current = fn;

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

  const reload = useCallback(() => {
    const req = ++reqRef.current;
    setBusy(true);
    setError("");
    fnRef
      .current()
      .then((d) => {
        if (mountedRef.current && req === reqRef.current) setData(d);
      })
      .catch((e) => {
        if (mountedRef.current && req === reqRef.current) setError(e.message);
      })
      .finally(() => {
        if (mountedRef.current && req === reqRef.current) setBusy(false);
      });
  }, []);

  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(reload, deps);

  return { data, error, busy, reload, setData };
}

/** Minimal history-API router for the admin app's sections. */
export function usePath(): [string, (p: string) => void] {
  const [path, setPath] = useState(window.location.pathname);
  useEffect(() => {
    const onPop = () => setPath(window.location.pathname);
    window.addEventListener("popstate", onPop);
    return () => window.removeEventListener("popstate", onPop);
  }, []);
  const navigate = useCallback((p: string) => {
    window.history.pushState(null, "", p);
    setPath(p);
  }, []);
  return [path, navigate];
}
