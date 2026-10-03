export function startQuotaPolling(
  refresh: (signal: AbortSignal) => Promise<unknown>,
  interval = 10_000,
) {
  let stopped = false;
  let visibleRefreshPending = false;
  let controller: AbortController | null = null;
  const run = async () => {
    if (stopped || document.hidden || controller) return;
    const current = new AbortController();
    controller = current;
    try {
      await refresh(current.signal);
    } catch {
      /* callers present errors; retain last good data */
    } finally {
      if (controller === current) controller = null;
      if (visibleRefreshPending && !stopped && !document.hidden) {
        visibleRefreshPending = false;
        void run();
      }
    }
  };
  const visibility = () => {
    if (document.hidden) controller?.abort();
    else {
      if (controller) visibleRefreshPending = true;
      else void run();
    }
  };
  const timer = window.setInterval(() => void run(), interval);
  document.addEventListener('visibilitychange', visibility);
  void run();
  return () => {
    stopped = true;
    window.clearInterval(timer);
    document.removeEventListener('visibilitychange', visibility);
    controller?.abort();
  };
}
