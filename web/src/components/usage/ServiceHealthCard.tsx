import { useTranslation } from 'react-i18next';
import type { UsageActivityBlock, UsageActivityResponse } from '@/lib/types';
import { healthGreenThreshold } from '@/utils/usage/health';
import {
  ActivityHeatmapGrid,
  parseActivityTime,
} from './ActivityHeatmapGrid';
import styles from '@/pages/UsagePage.module.scss';

const HEALTH_LEVEL_COLORS = [
  '#ef4444',
  '#f97316',
  '#facc15',
  '#84cc16',
  '#22c55e',
] as const;

export const parseTime = parseActivityTime;

export function calculateHealthActivityLevel(successCount: number, failureCount: number): number {
  // Credential Health 的红/绿语义保持不变，并把中间告警区细分为橙、黄、浅绿。
  const success = Number.isFinite(successCount) && successCount > 0 ? successCount : 0;
  const failure = Number.isFinite(failureCount) && failureCount > 0 ? failureCount : 0;
  const total = success + failure;
  if (total === 0) return 0;
  const rate = success / total;
  if (rate < 0.5) return 1;
  if (rate < 0.65) return 2;
  if (rate < 0.8) return 3;
  if (rate < healthGreenThreshold(total)) return 4;
  return 5;
}

function healthLevelColor(successCount: number, failureCount: number): string | undefined {
  const level = calculateHealthActivityLevel(successCount, failureCount);
  return level > 0 ? HEALTH_LEVEL_COLORS[level - 1] : undefined;
}
export interface ServiceHealthCardProps {
  activity: UsageActivityResponse | null;
  loading: boolean;
  requestIdentity: string;
}

export function ServiceHealthCard({ activity, loading, requestIdentity }: ServiceHealthCardProps) {
  const { t } = useTranslation();
  const projectTimeZone = activity?.timezone?.trim() || undefined;
  const totalSuccess = Number(activity?.total_success ?? 0);
  const totalFailure = Number(activity?.total_failure ?? 0);
  const successRate = Number(activity?.success_rate ?? 0);
  const hasData = totalSuccess + totalFailure > 0;
  const healthCountsLabel = `${t('status_bar.success_short')} ${totalSuccess}, ${t('status_bar.failure_short')} ${totalFailure}`;
  const summaryColor = hasData ? healthLevelColor(totalSuccess, totalFailure) : undefined;
  const getSummary = (block: UsageActivityBlock) => block.success + block.failure > 0
    ? `${t('status_bar.success_short')} ${block.success}, ${t('status_bar.failure_short')} ${block.failure}`
    : t('status_bar.no_requests');

  return (
    <article className={styles.activityCard} aria-busy={loading}>
      <div className={styles.activityCardHeader}>
        <div className={styles.sectionTitleBlock}>
          <h3 className={styles.sectionTitle}>{t('usage_stats.service_health_title')}</h3>
          <p className={styles.sectionSubtitle}>{t('usage_stats.service_health_subtitle')}</p>
        </div>
        <div className={styles.activitySummary} data-activity-summary="health">
          <span className={styles.activitySummaryLabel}>{t('usage_stats.success_rate')}</span>
          <strong className={styles.activitySummaryValue} style={{ color: summaryColor }}>
            {hasData ? `${successRate.toFixed(1)}%` : '--'}
          </strong>
          <div className={styles.activitySummaryDetails} aria-label={healthCountsLabel}>
            <span className={styles.healthCountRow}>
              <span className={`${styles.healthCountDot} ${styles.healthCountDotSuccess}`} aria-hidden="true" />
              <span>{totalSuccess}</span>
            </span>
            <span className={styles.healthCountRow}>
              <span className={`${styles.healthCountDot} ${styles.healthCountDotFailure}`} aria-hidden="true" />
              <span>{totalFailure}</span>
            </span>
          </div>
        </div>
      </div>
      {activity?.blocks.length ? (
        <ActivityHeatmapGrid
          blocks={activity.blocks}
          timeZone={projectTimeZone}
          requestIdentity={requestIdentity}
          ariaLabel={healthCountsLabel}
          isIdle={(block) => calculateHealthActivityLevel(block.success, block.failure) === 0}
          getColor={(block) => healthLevelColor(block.success, block.failure)}
          getSummary={getSummary}
          renderTooltipStats={(block) => block.success + block.failure > 0 ? (
            <span className={styles.activityTooltipStats}>
              <span className={styles.healthTooltipSuccess}>{t('status_bar.success_short')} {block.success}</span>
              <span className={styles.healthTooltipFailure}>{t('status_bar.failure_short')} {block.failure}</span>
              <span className={styles.healthTooltipRate}>({(block.rate * 100).toFixed(1)}%)</span>
            </span>
          ) : (
            <span className={styles.activityTooltipStats}>{t('status_bar.no_requests')}</span>
          )}
        />
      ) : (
        <div className={styles.activityCardEmpty}>{loading ? t('common.loading') : t('usage_stats.recent_activity_empty')}</div>
      )}
      <div className={styles.activityLegend}>
        <span className={styles.activityLegendLabel}>{t('usage_stats.service_health_unhealthy')}</span>
        <div className={styles.activityLegendColors}>
          <span className={`${styles.activityLegendBlock} ${styles.activityHeatmapBlockIdle}`} data-health-level="0" />
          {HEALTH_LEVEL_COLORS.map((color, index) => (
            <span key={color} className={styles.activityLegendBlock} style={{ backgroundColor: color }} data-health-level={index + 1} />
          ))}
        </div>
        <span className={styles.activityLegendLabel}>{t('usage_stats.service_health_healthy')}</span>
      </div>
    </article>
  );
}
