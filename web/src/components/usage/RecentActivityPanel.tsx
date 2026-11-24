import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { PillSwitcher, SectionToolbar } from '@/components/ui';
import type { UsageActivityResponse, UsageActivityWindow } from '@/lib/types';
import {
  createActivityDateTimeFormatter,
  formatActivityDateTime,
  parseActivityTime,
} from './ActivityHeatmapGrid';
import { OverviewActivityCards } from './OverviewActivityCards';
import styles from '@/pages/UsagePage.module.scss';

const ACTIVITY_WINDOWS: readonly { value: UsageActivityWindow; labelKey: string }[] = [
  { value: 'day', labelKey: 'usage_stats.recent_activity_window_day' },
  { value: 'week', labelKey: 'usage_stats.recent_activity_window_week' },
  { value: 'month', labelKey: 'usage_stats.recent_activity_window_month' },
  { value: 'year', labelKey: 'usage_stats.recent_activity_window_year' },
];

export interface RecentActivityPanelProps {
  activity: UsageActivityResponse | null;
  loading: boolean;
  error?: string;
  window: UsageActivityWindow | null;
  windowIsCurrent: boolean;
  requestIdentity: string;
  onWindowChange: (window: UsageActivityWindow) => void;
}

export function RecentActivityPanel({
  activity,
  loading,
  error,
  window,
  windowIsCurrent,
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
    : error === 'AUTH_REQUIRED'
      ? t('auth.session_expired')
      : error;

  const windowOptions = useMemo(
    () => ACTIVITY_WINDOWS.map((option) => ({ value: option.value, label: t(option.labelKey) })),
    [t],
  );

  return (
    <section className={styles.recentActivitySection}>
      <SectionToolbar
        title={t('usage_stats.recent_activity_title')}
        className={styles.recentActivityToolbar}
        headingClassName={styles.recentActivityHeading}
        titleClassName={styles.recentActivityTitle}
        actionsClassName={styles.recentActivityToolbarActions}
        metaClassName={styles.recentActivityRange}
        meta={sharedWindowLabel || undefined}
        actions={(
          <PillSwitcher
            ariaLabel={t('usage_stats.recent_activity_window')}
            className={styles.recentActivityWindowSwitcher}
            optionClassName={styles.recentActivityWindowButton}
            activeOptionClassName={styles.recentActivityWindowButtonActive}
            value={window}
            options={windowOptions}
            onChange={(nextWindow) => {
              if (!windowIsCurrent || window !== nextWindow) {
                onWindowChange(nextWindow);
              }
            }}
          />
        )}
      />
      {displayError && <div className={styles.errorBox} role="alert">{displayError}</div>}
      <OverviewActivityCards activity={activity} loading={loading} requestIdentity={requestIdentity} />
    </section>
  );
}
