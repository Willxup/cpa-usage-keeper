import { useEffect, useId, useMemo, useRef, useState, type CSSProperties } from 'react';
import { useTranslation } from 'react-i18next';
import type { UsageComparisonItem, UsageOverviewComparisons } from '@/lib/types';
import { Card } from '@/components/ui/Card';
import { LoadingSpinner } from '@/components/ui/LoadingSpinner';
import { PortalTooltip, usePortalTooltip } from '@/components/ui/PortalTooltip';
import { formatCompactNumber, formatUsd } from '@/utils/usage';
import { USAGE_CHART_COMPOSITION_COLORS } from '@/utils/usage/chartConfig';
import { buildComparisonView, layoutComparisonTreemap, type ComparisonRow } from './usageComparisonData';
import { UsageShareList } from './UsageShareList';
import styles from './UsageComparisonCharts.module.scss';
import usageStyles from '@/pages/UsagePage.module.scss';

const EMPTY: UsageComparisonItem[] = [];
const OTHER_COLOR = { base: '#64748b', light: '#cbd5e1' };
const rowStyle = (row: ComparisonRow, index: number): CSSProperties => {
  const color = row.other ? OTHER_COLOR : USAGE_CHART_COMPOSITION_COLORS[index % USAGE_CHART_COMPOSITION_COLORS.length];
  return { '--usage-light': color.light } as CSSProperties;
};
const formatCost = (value: number | null) => value === null ? '—' : formatUsd(value);
const formatShare = (value: number | null) => value === null ? '—' : `${value.toFixed(1)}%`;

type UsageDimension = 'api_keys' | 'auth_files' | 'ai_providers';
const DIMENSION_KEYS: readonly UsageDimension[] = ['api_keys', 'auth_files', 'ai_providers'];

function ComparisonChart({ items, dimension, loading, dimensions, titleKey }: { items: UsageComparisonItem[]; dimension: 'models' | UsageDimension; loading: boolean; dimensions?: Partial<Record<UsageDimension, UsageComparisonItem[]>>; titleKey?: string }) {
  const { t } = useTranslation();
  const [activeDimension, setActiveDimension] = useState<UsageDimension>(dimension === 'models' ? 'api_keys' : dimension);
  const activeItems = dimensions ? (dimensions[activeDimension] ?? EMPTY) : items;
  const view = useMemo(() => buildComparisonView(activeItems, t('usage_stats.comparison_others')), [activeItems, t]);
  const shareItems = useMemo(() => view.rows.map(row => ({
    key: row.key, label: row.label, tokens: row.total_tokens, requests: row.requests, share: row.share, cost: row.cost,
    cacheRate: row.input_tokens > 0 ? row.cache_read_tokens / row.input_tokens * 100 : null,
  })), [view.rows]);
  const title = t(titleKey ?? `usage_stats.comparison_${dimension}`);
  const treeRef = useRef<HTMLDivElement>(null);
  const [treeSize, setTreeSize] = useState({width: 480, height: 224});
  const tooltip = usePortalTooltip();
  const { dismiss } = tooltip;
  useEffect(() => { dismiss(); }, [items, dismiss]);
  useEffect(() => {
    const element = treeRef.current;
    if (!element || typeof ResizeObserver === 'undefined') return;
    const observer = new ResizeObserver(([entry]) => {
      if (entry.contentRect.width > 0 && entry.contentRect.height > 0) {
        setTreeSize({width:entry.contentRect.width, height:entry.contentRect.height});
      }
    });
    observer.observe(element);
    return () => observer.disconnect();
  }, []);
  const rects = useMemo(() => layoutComparisonTreemap(view.rows, treeSize.width / treeSize.height), [view.rows, treeSize]);
  const tooltipLines = (row: ComparisonRow) => [
    row.label,
    `${t('usage_stats.comparison_tokens')}: ${formatCompactNumber(row.total_tokens)} · ${formatShare(row.share)}`,
    `${t('usage_stats.comparison_requests')}: ${row.requests.toLocaleString()} · ${t('usage_stats.comparison_failures')}: ${row.failures.toLocaleString()}`,
    `${t('usage_stats.comparison_input')}: ${formatCompactNumber(row.input_tokens)} · ${t('usage_stats.comparison_output')}: ${formatCompactNumber(row.output_tokens)}`,
    `${t('usage_stats.comparison_cache_read')}: ${formatCompactNumber(row.cache_read_tokens)} · ${t('usage_stats.comparison_cache_write')}: ${formatCompactNumber(row.cache_creation_tokens)}`,
    `${t('usage_stats.comparison_reasoning')}: ${formatCompactNumber(row.reasoning_tokens)}`,
    `${t('usage_stats.comparison_cost')}: ${formatCost(row.cost)}`,
  ];
  const events = (row: ComparisonRow) => ({
    onMouseEnter: (event: React.MouseEvent<HTMLButtonElement>) => tooltip.showOnMouseEnter(tooltipLines(row), event.currentTarget),
    onMouseLeave: (event: React.MouseEvent<HTMLButtonElement>) => tooltip.hideOnMouseLeave(event.currentTarget),
    onFocus: (event: React.FocusEvent<HTMLButtonElement>) => tooltip.showOnFocus(tooltipLines(row), event.currentTarget),
    onBlur: (event: React.FocusEvent<HTMLButtonElement>) => tooltip.hideOnBlur(event.currentTarget),
    onClick: (event: React.MouseEvent<HTMLButtonElement>) => tooltip.showOnFocus(tooltipLines(row), event.currentTarget),
    onKeyDown: (event: React.KeyboardEvent<HTMLButtonElement>) => { if (event.key === 'Escape') tooltip.dismiss(); },
  });
  const empty = loading && items.length === 0 ? <><LoadingSpinner size={18} />{t('common.loading')}</> : t('usage_stats.comparison_empty');

  return <Card title={title} className={styles.card} data-comparison={dimension}>
    {dimension === 'models' ? <div className={styles.chartSurface} aria-busy={loading}>
      <div ref={treeRef} className={styles.treemap} aria-label={title}>
        {rects.length === 0 ? <div className={styles.empty}>{empty}</div> : rects.map((rect, index) => {
          const tiny = rect.width * treeSize.width / 100 < 48 || rect.height * treeSize.height / 100 < 28;
          return <div key={rect.row.key} className={styles.tileSlot} style={{left:`${rect.x}%`, top:`${rect.y}%`, width:`${rect.width}%`, height:`${rect.height}%`}}>
            <button type="button" data-comparison-entry={rect.row.key} className={styles.tile} style={rowStyle(rect.row,index)} aria-label={tooltipLines(rect.row).join(', ')} data-tiny={tiny} {...events(rect.row)}>
              <span className={styles.tileName}>{rect.row.label}</span>
            </button>
          </div>;
        })}
      </div>
    </div> : <>
      {dimensions && <div className={usageStyles.overviewRealtimeDimensionTabs} role="tablist">
        {DIMENSION_KEYS.map((key) => <button key={key} type="button" className={`${usageStyles.overviewRealtimeDimensionTab} ${activeDimension === key ? usageStyles.overviewRealtimeDimensionTabActive : ''}`.trim()} onClick={() => setActiveDimension(key)} aria-pressed={activeDimension === key}>{t(`usage_stats.overview_realtime_dimension_${key}`)}</button>)}
      </div>}
      <UsageShareList items={shareItems} loading={loading} emptyContent={empty} />
    </>}
    <PortalTooltip tooltip={tooltip.tooltip} />
  </Card>;
}

export function UsageComparisonCharts({ comparisons, loading, keyViewer = false }: { comparisons?: UsageOverviewComparisons; loading: boolean; keyViewer?: boolean }) {
  const { t } = useTranslation();
  const headingId = useId();
  return <section className={usageStyles.recentActivitySection} aria-labelledby={headingId}>
    <div className={usageStyles.recentActivityHeading}>
      <h2 id={headingId} className={usageStyles.recentActivityTitle}>{t('usage_stats.comparison_title')}</h2>
    </div>
    <div className={styles.grid}>
      <ComparisonChart dimension="models" items={comparisons?.models ?? EMPTY} loading={loading} />
      <ComparisonChart
        dimension="api_keys"
        items={comparisons?.api_keys ?? EMPTY}
        dimensions={keyViewer ? undefined : { api_keys: comparisons?.api_keys ?? EMPTY, auth_files: comparisons?.auth_files ?? EMPTY, ai_providers: comparisons?.ai_providers ?? EMPTY }}
        titleKey={keyViewer ? 'usage_stats.comparison_api_keys' : 'usage_stats.comparison_token_usage'}
        loading={loading}
      />
    </div>
  </section>;
}
