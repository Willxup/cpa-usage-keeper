import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { ApiError, fetchAdminProviderQuota, fetchKeyQuota, fetchReadOnlyQuota, type ViewerQuotaAccount, type ViewerQuotaRow } from '@/lib/api';
import { startQuotaPolling } from '@/lib/quotaPolling';
import { PortalTooltip, usePortalTooltip } from '@/components/ui/PortalTooltip';
import { quotaSourceLabel } from './quotaSourceLabel';
import cardStyles from '@/pages/UsagePage.module.scss';
import styles from './OverviewQuotaPanel.module.scss';

export type QuotaScope = 'admin' | 'key' | 'readOnly';

export function overviewQuotaRows(account: ViewerQuotaAccount): ViewerQuotaRow[] {
  return account.rows.filter(row => !/reserve/i.test(`${row.label} ${row.metric ?? ''}`));
}

export function remainingPercent(row: ViewerQuotaRow): number | null {
  const percent = row.remaining_percent ?? (
    row.remaining != null && row.limit != null && row.limit > 0 ? row.remaining / row.limit * 100 : null
  );
  return percent != null && Number.isFinite(percent) ? Math.max(0, Math.min(100, percent)) : null;
}

export function resetCountdown(resetAt: string | undefined, now: number): string | null {
  const reset = resetAt ? Date.parse(resetAt) : NaN;
  if (!Number.isFinite(reset)) return null;
  const minutes = Math.ceil((reset - now) / 60_000);
  if (minutes <= 0) return '0m';
  if (minutes >= 1440) return `${Math.floor(minutes / 1440)}d ${Math.floor(minutes % 1440 / 60)}h ${minutes % 60}m`;
  if (minutes >= 60) return `${Math.floor(minutes / 60)}h ${minutes % 60}m`;
  return `${minutes}m`;
}

function windowLabel(label: string): string {
  return label.replace(/Weekly/gi, '7d').replace(/Monthly/gi, '30d');
}

export function observationAge(captured: number, now: number): { minutes: number; seconds: string } {
  const seconds = Math.max(0, Math.floor((now - captured) / 1000));
  return { minutes: Math.floor(seconds / 60), seconds: String(seconds % 60).padStart(2, '0') };
}

export function localizedResetDate(reset: number): string {
  return new Intl.DateTimeFormat(undefined, {
    year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit',
  }).format(new Date(reset));
}

export function OverviewQuotaPanel({ scope = 'admin', onAuthRequired }: {
  scope?: QuotaScope; onAuthRequired?: () => void;
}) {
  const { t } = useTranslation();
  const [accounts, setAccounts] = useState<ViewerQuotaAccount[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [error, setError] = useState(false);
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => startQuotaPolling(async signal => {
    try {
      const fetchQuota = scope === 'admin' ? fetchAdminProviderQuota : scope === 'readOnly' ? fetchReadOnlyQuota : fetchKeyQuota;
      const data = await fetchQuota(signal);
      if (!signal.aborted) { setAccounts(data.accounts); setLoaded(true); setError(false); setNow(Date.now()); }
    } catch (err) {
      if (!signal.aborted) {
        if (err instanceof ApiError && err.status === 401) onAuthRequired?.();
        setError(true);
      }
    }
  }), [scope, onAuthRequired]);
  useEffect(() => {
    const timer = window.setInterval(() => { if (!document.hidden) setNow(Date.now()); }, 1000);
    return () => window.clearInterval(timer);
  }, []);
  return <OverviewQuotaTable accounts={accounts} now={now} notice={error ? t('key_quota.error') : !loaded ? t('common.loading') : undefined} />;
}

export function OverviewQuotaTable({ accounts, now, notice }: {
  accounts: ViewerQuotaAccount[]; now: number; notice?: string;
}) {
  const { t } = useTranslation();
  const { tooltip, showOnMouseEnter, hideOnMouseLeave, showOnFocus, hideOnBlur, dismiss } = usePortalTooltip();
  const tooltipDetails = new Map<string, string[]>();
  return <section className={`${cardStyles.statCard} ${styles.panel}`} aria-label={t('overview_limits.title')}>
    <div className={cardStyles.statCardHeader}><span className={cardStyles.statLabel}>{t('overview_limits.title')}</span></div>
    {notice && <span className={styles.notice} role="status">{notice}</span>}
    <div className={styles.scroll} role="region" aria-label={t('overview_limits.title')} tabIndex={0}>
      <table>
        <thead><tr><th>{t('overview_limits.account')}</th><th><div className={styles.quotaHeading}><span>{t('overview_limits.window')}</span><span>{t('overview_limits.remaining')}</span></div></th><th>{t('overview_limits.resets')}</th></tr></thead>
        <tbody>{accounts.flatMap(account => {
          const rows = overviewQuotaRows(account);
          if (rows.length === 0) return [<tr key={`${account.kind}:${account.label}`}>
            <td><strong className={styles.account}>{account.label}</strong></td>
            <td><span className={styles.notice}>{t('overview_limits.unavailable')}</span></td><td>—</td>
          </tr>];
          return rows.map((row, index) => {
              const percent = remainingPercent(row);
              const at = row.captured_at ?? account.updated_at;
              const captured = at ? Date.parse(at) : NaN;
              const reset = row.reset_at ? Date.parse(row.reset_at) : NaN;
              const stale = row.stale || !Number.isFinite(captured) || now - captured > 900_000 || reset <= now;
              const label = windowLabel(row.label);
              const tooltipKey = `${account.kind}:${account.provider}:${account.label}:${index}`;
              const details = [account.label, `${t('overview_limits.window')}: ${label}`,
                ...(Number.isFinite(reset) ? [`${t('key_quota.resets')}: ${localizedResetDate(reset)}`] : []),
                Number.isFinite(captured) ? `${t('key_quota.updated')}: ${t('overview_limits.observed_ago', observationAge(captured, now))}` : t('overview_limits.capture_unknown'),
                quotaSourceLabel(row.source),
                ...(stale ? [t('overview_limits.stale')] : []),
              ];
              tooltipDetails.set(tooltipKey, details);
              const value = percent != null ? `${percent.toLocaleString(undefined, { maximumFractionDigits: 1 })}%`
                : row.remaining != null ? `${row.remaining.toLocaleString(undefined, { maximumFractionDigits: 2 })}${row.limit != null ? ` / ${row.limit.toLocaleString()}` : ''}` : '—';
              return <tr key={`${account.kind}:${account.label}:${index}`} className={index > 0 ? styles.continuation : undefined}>
                {index === 0 && <td rowSpan={rows.length}><strong className={styles.account}>{account.label}</strong></td>}
                <td><div className={styles.limit}>
                <span className={styles.window}>{label}{row.metric && row.metric !== 'window' && <span className={styles.metric}> · {row.metric}</span>}</span>
                {percent != null ? <progress className={percent < 25 || row.limit_reached ? styles.low : undefined} max={100} value={percent} aria-label={`${account.label} ${label}: ${t('overview_limits.remaining')}`} /> : <span />}
                  <span className={styles.value}><strong>{value}</strong><button type="button" className={`${styles.info} ${stale ? styles.stale : ''}`}
                    aria-label={`${t('overview_limits.info')}: ${details.join(' · ')}`}
                    onMouseEnter={event => showOnMouseEnter([tooltipKey], event.currentTarget)} onMouseLeave={event => hideOnMouseLeave(event.currentTarget)}
                    onFocus={event => showOnFocus([tooltipKey], event.currentTarget)} onBlur={event => hideOnBlur(event.currentTarget)}
                    onClick={event => showOnFocus([tooltipKey], event.currentTarget)} onKeyDown={event => { if (event.key === 'Escape') dismiss(); }}>ⓘ</button></span>
              </div></td>
            <td><div className={styles.reset}
              aria-label={`${windowLabel(row.label)}: ${t('overview_limits.resets')}`}>
              {row.reset_at && Date.parse(row.reset_at) <= now ? t('overview_limits.awaiting') : resetCountdown(row.reset_at, now) ?? '—'}
            </div></td>
          </tr>;
          });
        })}</tbody>
      </table>
      {!notice && accounts.length === 0 && <span className={styles.notice}>{t('key_quota.empty')}</span>}
    </div>
    <PortalTooltip tooltip={tooltip && tooltipDetails.has(tooltip.lines[0]) ? { ...tooltip, lines: tooltipDetails.get(tooltip.lines[0])! } : null} />
  </section>;
}
