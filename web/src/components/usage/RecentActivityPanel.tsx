import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import type { UsageActivityResponse, UsageActivityWindow } from '@/lib/types';
import {
  createActivityDateTimeFormatter,
  formatActivityDateTime,
  parseActivityTime,
} from './ActivityHeatmapGrid';
import { OverviewActivityCards } from './OverviewActivityCards';
import styles from '@/pages/UsagePage.module.scss';

const ACTIVITY_WINDOWS: readonly UsageActivityWindow[] = ['24h', '7d', '30d', '1y'];

export interface RecentActivityPanelProps {
  activity: UsageActivityResponse | null;
  loading: boolean;
  error?: string;
  window: UsageActivityWindow | null;
  requestIdentity: string;
  onWindowChange: (window: UsageActivityWindow) => void;
}

export function RecentActivityPanel({
  activity,
  loading,
  error,
  window,
  requestIdentity,
  onWindowChange,
}: RecentActivityPanelProps) {
  const { t } = useTranslation();
  const projectTimeZone = activity?.timezone?.trim() || undefined;
  const dateTimeFormatter = useMemo(() => createActivityDateTimeFormatter(projectTimeZone), [projectTimeZone]);
  const windowStart = parseActivityTime(activity?.window_start);
  const windowEnd = parseActivityTime(activity?.window_end);
  const sharedWindowLabel = windowStart > 0 && windowEnd > 0
    ? `${formatActivityDateTime(windowStart, dateTimeFormatter)} – ${formatActivityDateTime(windowEnd, dateTimeFormatter)}`
    : '';
  const displayError = error === 'ACTIVITY_LOAD_FAILED'
    ? t('usage_stats.recent_activity_load_failed')
    : error === 'KEY_ACTIVITY_RATE_LIMITED'
      ? t('usage_stats.recent_activity_rate_limited')
      : error === 'AUTH_REQUIRED'
        ? t('auth.session_expired')
        : error;

  return (
    <section className={styles.recentActivitySection}>
      <div className={styles.recentActivityToolbar}>
        <div className={styles.recentActivityHeading}>
          <h2 className={styles.recentActivityTitle}>{t('usage_stats.recent_activity_title')}</h2>
        </div>
        <div className={styles.recentActivityToolbarActions}>
          {sharedWindowLabel && <span className={styles.recentActivityRange}>{sharedWindowLabel}</span>}
          <div className={styles.recentActivityWindowSwitcher} role="group" aria-label={t('usage_stats.recent_activity_window')}>
            {ACTIVITY_WINDOWS.map((option) => (
              <button
                key={option}
                type="button"
                className={`${styles.recentActivityWindowButton} ${window === option ? styles.recentActivityWindowButtonActive : ''}`.trim()}
                onClick={() => onWindowChange(option)}
                aria-pressed={window === option}
              >
                {option}
              </button>
            ))}
          </div>
        </div>
      </div>
      {displayError && <div className={styles.errorBox} role="alert">{displayError}</div>}
      <OverviewActivityCards activity={activity} loading={loading} requestIdentity={requestIdentity} />
    </section>
  );
}
