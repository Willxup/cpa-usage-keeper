import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Select } from '@/components/ui/Select';
import styles from './AutoRefreshControl.module.scss';

export const AUTO_REFRESH_STORAGE_KEY = 'keeper-auto-refresh-seconds';
const INTERVALS = [0, 10, 30, 60] as const;

interface AutoRefreshControlProps {
  enabled?: boolean;
  busy?: boolean;
  onRefresh: () => Promise<unknown>;
  onError: (error: unknown) => void;
}

/** Own the clock here so countdown ticks never rerender the charts. */
export function AutoRefreshControl({ enabled = true, busy = false, onRefresh, onError }: AutoRefreshControlProps) {
  const { t } = useTranslation();
  const [seconds, setSeconds] = useState(() => {
    try {
      const saved = localStorage.getItem(AUTO_REFRESH_STORAGE_KEY);
      const value = saved === null ? 10 : Number(saved);
      return INTERVALS.some((interval) => interval === value) ? value : 10;
    } catch { return 10; }
  });
  const [remaining, setRemaining] = useState(seconds);
  const [hidden, setHidden] = useState(document.hidden);
  const [refreshing, setRefreshing] = useState(false);
  const current = useRef({ busy, onRefresh, onError });
  const inFlight = useRef(false);

  useEffect(() => { current.current = { busy, onRefresh, onError }; }, [busy, onRefresh, onError]);

  useEffect(() => {
    if (!enabled || seconds === 0) return;
    let disposed = false;
    let timer: ReturnType<typeof setInterval> | undefined;
    let deadline = Date.now() + seconds * 1000;
    const reset = () => {
      deadline = Date.now() + seconds * 1000;
      if (!disposed) setRemaining(seconds);
    };
    const refresh = async () => {
      if (inFlight.current || current.current.busy) return;
      inFlight.current = true;
      setRefreshing(true);
      try { await current.current.onRefresh(); }
      catch (error) { if (!disposed) current.current.onError(error); }
      finally {
        inFlight.current = false;
        if (!disposed) { setRefreshing(false); reset(); }
      }
    };
    const stop = () => { clearInterval(timer); timer = undefined; };
    const start = () => {
      stop();
      reset();
      timer = setInterval(() => {
        // A slow request or a manual reload starts a fresh countdown when it finishes.
        setRefreshing(inFlight.current);
        if (inFlight.current || current.current.busy) { reset(); return; }
        const left = Math.max(0, Math.ceil((deadline - Date.now()) / 1000));
        setRemaining(left);
        if (left === 0) void refresh();
      }, 250);
    };
    const visibilityChanged = () => {
      setHidden(document.hidden);
      if (document.hidden) stop();
      else { start(); void refresh(); }
    };
    setHidden(document.hidden);
    setRefreshing(inFlight.current);
    if (!document.hidden) start();
    document.addEventListener('visibilitychange', visibilityChanged);
    return () => { disposed = true; stop(); document.removeEventListener('visibilitychange', visibilityChanged); };
  }, [enabled, seconds]);

  const off = t('auto_refresh.off');
  const label = seconds === 0 ? off : !enabled || hidden ? t('auto_refresh.paused') : refreshing || busy ? '…' : `${remaining}s`;
  return (
    <span className={styles.control} data-auto-refresh title={t('auto_refresh.settings')}>
      <Select
        value={String(seconds)}
        options={INTERVALS.map((interval) => ({ value: String(interval), label: interval === 0 ? off : t('auto_refresh.every', { seconds: interval }) }))}
        onChange={(value) => {
          const next = Number(value);
          setRemaining(next);
          setSeconds(next);
          try { localStorage.setItem(AUTO_REFRESH_STORAGE_KEY, value); } catch { /* Session preference still works. */ }
        }}
        ariaLabel={t('auto_refresh.settings')}
        fullWidth={false}
        dropdownMinWidth={160}
        showChevron={false}
        renderValue={() => <span aria-hidden="true">{label}</span>}
      />
    </span>
  );
}
