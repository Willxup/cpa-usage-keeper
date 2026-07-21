import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import type { UsageActivityBlock, UsageActivityResponse } from '@/lib/types';
import { formatCompactNumber } from '@/utils/usage';
import { ActivityHeatmapGrid } from './ActivityHeatmapGrid';
import styles from '@/pages/UsagePage.module.scss';

export function calculateTokenActivityLevels(values: readonly number[]): number[] {
  const positiveValues = values.filter((value) => Number.isFinite(value) && value > 0).sort((left, right) => left - right);
  if (positiveValues.length === 0) return values.map(() => 0);
  // 用非零格子的 P5–P95 作为可见范围，避免少量极端值把其余格子压进同一色阶。
  const low = positiveValues[Math.max(0, Math.ceil(positiveValues.length * 0.05) - 1)];
  const high = positiveValues[Math.max(0, Math.ceil(positiveValues.length * 0.95) - 1)];
  if (low === high) return values.map((value) => (Number.isFinite(value) && value > 0 ? 5 : 0));
  const logLow = Math.log1p(low);
  const logRange = Math.log1p(high) - logLow;
  return values.map((value) => {
    if (!Number.isFinite(value) || value <= 0) return 0;
    const clamped = Math.max(low, Math.min(value, high));
    const ratio = (Math.log1p(clamped) - logLow) / logRange;
    return Math.max(1, Math.min(5, 1 + Math.floor(ratio * 5)));
  });
}

export interface TokenActivityCardProps {
  activity: UsageActivityResponse | null;
  loading: boolean;
  requestIdentity: string;
}

export function TokenActivityCard({ activity, loading, requestIdentity }: TokenActivityCardProps) {
  const { t, i18n } = useTranslation();
  const blocks = useMemo(() => activity?.blocks ?? [], [activity]);
  const levels = useMemo(() => calculateTokenActivityLevels(blocks.map((block) => block.total_tokens)), [blocks]);
  const projectTimeZone = activity?.timezone?.trim() || undefined;
  const numberFormatter = useMemo(() => {
    try {
      return new Intl.NumberFormat(i18n.resolvedLanguage || i18n.language);
    } catch {
      return new Intl.NumberFormat();
    }
  }, [i18n.language, i18n.resolvedLanguage]);
  const formatTokenSummary = (block: UsageActivityBlock) => [
    `${t('usage_stats.token_activity_total')} ${numberFormatter.format(block.total_tokens)}`,
    `${t('usage_stats.token_activity_input')} ${numberFormatter.format(block.input_tokens)}`,
    `${t('usage_stats.token_activity_output')} ${numberFormatter.format(block.output_tokens)}`,
    `${t('usage_stats.token_activity_reasoning')} ${numberFormatter.format(block.reasoning_tokens)}`,
    `${t('usage_stats.token_activity_cache_read')} ${numberFormatter.format(block.cache_read_tokens)}`,
    `${t('usage_stats.token_activity_cache_creation')} ${numberFormatter.format(block.cache_creation_tokens)}`,
  ].join(', ');

  return (
    <article className={`${styles.activityCard} ${styles.tokenActivityCard}`} aria-busy={loading}>
      <div className={styles.activityCardHeader}>
        <div className={styles.sectionTitleBlock}>
          <h3 className={styles.sectionTitle}>{t('usage_stats.token_activity_title')}</h3>
          <p className={styles.sectionSubtitle}>{t('usage_stats.token_activity_subtitle')}</p>
        </div>
        <div className={styles.activitySummary} data-activity-summary="token">
          <span className={styles.activitySummaryLabel}>{t('usage_stats.token_activity_total_tokens')}</span>
          <strong className={`${styles.activitySummaryValue} ${styles.tokenActivitySummaryValue}`} aria-label={`${t('usage_stats.token_activity_total_tokens')} ${numberFormatter.format(activity?.total_tokens ?? 0)}`}>
            {activity ? formatCompactNumber(activity.total_tokens) : '--'}
          </strong>
          <span className={styles.activitySummaryDetails}>
            <span>{t('usage_stats.token_activity_input')} {formatCompactNumber(activity?.input_tokens ?? 0)}</span>
            <span>{t('usage_stats.token_activity_output')} {formatCompactNumber(activity?.output_tokens ?? 0)}</span>
          </span>
        </div>
      </div>
      {blocks.length ? (
        <ActivityHeatmapGrid
          blocks={blocks}
          timeZone={projectTimeZone}
          requestIdentity={requestIdentity}
          ariaLabel={`${t('usage_stats.token_activity_title')}: ${t('usage_stats.token_activity_total_tokens')} ${numberFormatter.format(activity?.total_tokens ?? 0)}`}
          isIdle={(_block, index) => levels[index] === 0}
          getColor={(_block, index) => `var(--token-activity-level-${levels[index]})`}
          getSummary={formatTokenSummary}
          renderTooltipStats={(block) => (
            <span className={styles.tokenActivityTooltipStats}>
              <span>{t('usage_stats.token_activity_total')} <strong>{formatCompactNumber(block.total_tokens)}</strong></span>
              <span>{t('usage_stats.token_activity_input')} <strong>{formatCompactNumber(block.input_tokens)}</strong></span>
              <span>{t('usage_stats.token_activity_output')} <strong>{formatCompactNumber(block.output_tokens)}</strong></span>
              <span>{t('usage_stats.token_activity_reasoning')} <strong>{formatCompactNumber(block.reasoning_tokens)}</strong></span>
              <span>{t('usage_stats.token_activity_cache_read')} <strong>{formatCompactNumber(block.cache_read_tokens)}</strong></span>
              <span>{t('usage_stats.token_activity_cache_creation')} <strong>{formatCompactNumber(block.cache_creation_tokens)}</strong></span>
            </span>
          )}
        />
      ) : (
        <div className={styles.activityCardEmpty}>{loading ? t('common.loading') : t('usage_stats.recent_activity_empty')}</div>
      )}
      <div className={styles.activityLegend}>
        <span className={styles.activityLegendLabel}>{t('usage_stats.token_activity_less')}</span>
        <div className={styles.activityLegendColors}>
          <span className={`${styles.activityLegendBlock} ${styles.activityHeatmapBlockIdle}`} />
          {[1, 2, 3, 4, 5].map((level) => (
            <span key={level} className={styles.activityLegendBlock} style={{ backgroundColor: `var(--token-activity-level-${level})` }} />
          ))}
        </div>
        <span className={styles.activityLegendLabel}>{t('usage_stats.token_activity_more')}</span>
      </div>
    </article>
  );
}
