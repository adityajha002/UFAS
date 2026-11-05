import { useEffect, useRef } from "react";

/** Runs fn now and then every intervalMs while `enabled`.
 *  Hidden tab: paused, or slowed to hiddenMs when given.
 *  Coming back to the tab triggers an immediate run. fn handles its own errors. */
export function usePolling(fn: () => Promise<unknown> | void, intervalMs: number, enabled = true, hiddenMs: number | null = null, deps: unknown[] = []) {
  const ref = useRef(fn);
  ref.current = fn;
  useEffect(() => {
    if (!enabled) return;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let dead = false;
    const next = () => {
      if (dead) return;
      const hidden = document.hidden;
      if (hidden && hiddenMs == null) { timer = undefined; return; } // resumed by visibilitychange
      timer = setTimeout(tick, hidden ? (hiddenMs as number) : intervalMs);
    };
    const tick = async () => {
      if (dead) return;
      try { await ref.current(); } catch { /* the caller reports errors */ }
      next();
    };
    const onVisible = () => {
      if (document.hidden || dead) return;
      if (timer) clearTimeout(timer);
      void tick();
    };
    document.addEventListener("visibilitychange", onVisible);
    window.addEventListener("focus", onVisible);
    void tick();
    return () => {
      dead = true;
      if (timer) clearTimeout(timer);
      document.removeEventListener("visibilitychange", onVisible);
      window.removeEventListener("focus", onVisible);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [intervalMs, enabled, hiddenMs, ...deps]);
}
