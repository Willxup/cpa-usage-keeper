import { useEffect, useId, useMemo, useRef, useState, type CSSProperties, type KeyboardEvent } from 'react';
import { useTranslation } from 'react-i18next';
import '@/lib/chartjs';
import { Line } from 'react-chartjs-2';
import type { Chart as ChartJS, ChartData, ChartOptions, TooltipModel } from 'chart.js';
import type { UsageComparisonItem, UsageOverviewComparisons } from '@/lib/types';
import { Card } from '@/components/ui/Card';
import { LoadingSpinner } from '@/components/ui/LoadingSpinner';
import { PortalTooltip, usePortalTooltip } from '@/components/ui/PortalTooltip';
import { formatCompactNumber, formatUsd } from '@/utils/usage';
import { getUsageChartTheme } from '@/utils/usage/chartConfig';
import { buildComparisonView, formatComparisonBucket, type ComparisonRow } from './usageComparisonData';
import styles from './UsageComparisonCharts.module.scss';
import usageStyles from '@/pages/UsagePage.module.scss';

const EMPTY: UsageComparisonItem[] = [];
const EMPTY_BUCKETS: string[] = [];
const COLORS = ['#cfe3fc', '#387ff5', '#234f91', '#8950ee', '#b69af5', '#94a3b8'];
type UsageDimension = 'models' | 'api_keys' | 'auth_files' | 'ai_providers';
const DIMENSIONS: readonly UsageDimension[] = ['models', 'api_keys', 'auth_files', 'ai_providers'];
const colorFor = (row: ComparisonRow, index: number) => COLORS[row.other ? 5 : index];
const formatShare = (value: number | null) => value === null ? '—' : `${value.toFixed(1)}%`;
const formatCost = (value: number | null) => value === null ? '—' : formatUsd(value);

interface ChartProps {
  comparisons?: UsageOverviewComparisons;
  loading: boolean;
  keyViewer?: boolean;
  isDark?: boolean;
  isMobile?: boolean;
}

// 主页面与 Key Viewer 共用单卡片，维度边界仍由服务端权限和这里的可选项共同限制。
export function UsageComparisonCharts({ comparisons, loading, keyViewer = false, isDark = false, isMobile = false }: ChartProps) {
  const { t } = useTranslation();
  const headingId = useId();
  const [selected, setSelected] = useState<UsageDimension>('models');
  const dimension = keyViewer && (selected === 'auth_files' || selected === 'ai_providers') ? 'models' : selected;
  const dimensions = keyViewer ? DIMENSIONS.slice(0, 2) : DIMENSIONS;
  const items = comparisons?.[dimension] ?? EMPTY;
  const total = items.reduce((sum, item) => sum + item.total_tokens, 0);
  return <section className={usageStyles.recentActivitySection} aria-labelledby={headingId}>
    <div className={usageStyles.recentActivityToolbar}>
      <div className={usageStyles.recentActivityHeading}>
        <h2 id={headingId} className={usageStyles.recentActivityTitle}>{t('usage_stats.comparison_title')}</h2>
      </div>
      <div className={usageStyles.recentActivityToolbarActions}>
        <div className={`${usageStyles.recentActivityWindowSwitcher} ${styles.dimensions}`} role="group" aria-label={t('usage_stats.comparison_title')}>
          {dimensions.map(key => <button key={key} type="button" data-dimension={key} aria-pressed={dimension === key}
            className={`${usageStyles.recentActivityWindowButton} ${dimension === key ? usageStyles.recentActivityWindowButtonActive : ''}`}
            onClick={() => setSelected(key)}>{t(`usage_stats.overview_realtime_dimension_${key}`)}</button>)}
        </div>
      </div>
    </div>
    <Card title={t('usage_stats.comparison_token_trend')} className={styles.card} extra={
      <span className={styles.total} data-comparison-total><strong>{items.length || !loading ? formatCompactNumber(total) : '—'}</strong><span>{t('usage_stats.comparison_tokens')}</span></span>
    }>
      <ComparisonArea key={dimension} dimension={dimension} comparisons={comparisons} loading={loading} isDark={isDark} isMobile={isMobile} />
    </Card>
  </section>;
}

function ComparisonArea({ dimension, comparisons, loading, isDark, isMobile }: ChartProps & {dimension: UsageDimension}) {
  const { t, i18n } = useTranslation();
  const items = comparisons?.[dimension] ?? EMPTY;
  const buckets = comparisons?.buckets ?? EMPTY_BUCKETS;
  const view = useMemo(() => buildComparisonView(items, t('usage_stats.comparison_others')), [items, t]);
  const granularity = comparisons?.granularity ?? 'hourly';
  const labels = useMemo(() => buckets.map(bucket => formatComparisonBucket(bucket, granularity, comparisons?.timezone, i18n.language)), [buckets, granularity, comparisons?.timezone, i18n.language]);
  const fullLabels = useMemo(() => buckets.map(bucket => formatComparisonBucket(bucket, granularity, comparisons?.timezone, i18n.language, true)), [buckets, granularity, comparisons?.timezone, i18n.language]);
  const [hidden, setHidden] = useState<Set<string>>(() => new Set());
  const chartRef = useRef<ChartJS<'line'>>(null);
  const tooltipRef = useRef<HTMLDivElement>(null);
  const keyboardIndex = useRef(0);
  const detailsTooltip = usePortalTooltip();
  const { dismiss } = detailsTooltip;
  const theme = useMemo(() => getUsageChartTheme(Boolean(isDark)), [isDark]);
  const peak = useMemo(() => buckets.reduce((max, _, index) => Math.max(max,
    view.rows.reduce((sum, row) => sum + (hidden.has(row.key) ? 0 : row.token_series?.[index] ?? 0), 0)), 0), [buckets, view.rows, hidden]);

  useEffect(() => {
    dismiss();
    if (tooltipRef.current) tooltipRef.current.hidden = true;
    const chart = chartRef.current;
    if (chart) {
      chart.setActiveElements([]);
      chart.tooltip?.setActiveElements([], {x: 0, y: 0});
      chart.update('none');
    }
  }, [items, buckets, hidden, dismiss]);

  const data = useMemo<ChartData<'line', number[], string>>(() => ({
    labels,
    datasets: view.rows.map((row, index) => ({
      key: row.key,
      label: row.label,
      data: buckets.map((_, bucketIndex) => row.token_series?.[bucketIndex] ?? 0),
      backgroundColor: colorFor(row, index), borderColor: colorFor(row, index), borderWidth: 0,
      fill: index === 0 ? 'origin' : '-1', cubicInterpolationMode: 'monotone',
      pointRadius: buckets.length === 1 ? 4 : 0, pointHoverRadius: 4,
      pointHoverBackgroundColor: colorFor(row, index), pointHoverBorderColor: theme.tooltipBg, pointHoverBorderWidth: 2,
      hidden: hidden.has(row.key),
    })),
  }), [labels, buckets, view.rows, hidden, theme.tooltipBg]);

  const options = useMemo<ChartOptions<'line'>>(() => ({
    responsive: true, maintainAspectRatio: false, animation: false,
    interaction: {mode: 'index', intersect: false},
    layout: {padding: {top: 12, right: 4}},
    plugins: {
      legend: {display: false},
      // 用 HTML 实现参考图的分列、圆角和阴影；只写 textContent，标签不作为 HTML 解析。
      tooltip: {enabled: false, external: ({chart, tooltip}: {chart: ChartJS; tooltip: TooltipModel<'line'>}) => {
        const element = tooltipRef.current;
        if (!element) return;
        if (!tooltip.opacity || !tooltip.dataPoints?.length) { element.hidden = true; return; }
        element.replaceChildren();
        const title = document.createElement('div');
        title.className = styles.tooltipTitle;
        title.textContent = fullLabels[tooltip.dataPoints[0].dataIndex];
        element.append(title);
        for (const point of tooltip.dataPoints) {
          const row = document.createElement('div');
          row.className = styles.tooltipRow;
          const swatch = document.createElement('i');
          swatch.style.background = colorFor(view.rows[point.datasetIndex], point.datasetIndex);
          const name = document.createElement('span');
          name.textContent = point.dataset.label ?? '';
          const value = document.createElement('strong');
          value.textContent = formatCompactNumber(Number(point.raw));
          row.append(swatch, name, value);
          element.append(row);
        }
        element.hidden = false;
        const left = tooltip.caretX + 16 + element.offsetWidth > chart.width ? tooltip.caretX - element.offsetWidth - 16 : tooltip.caretX + 16;
        element.style.left = `${Math.max(0, Math.min(left, chart.width - element.offsetWidth))}px`;
        element.style.top = `${Math.max(0, Math.min(tooltip.caretY - 20, chart.height - element.offsetHeight))}px`;
      }},
    },
    scales: {
      x: {offset: buckets.length === 1, border: {display: false}, grid: {display: false},
        ticks: {color: theme.textSecondary, maxRotation: 0, maxTicksLimit: isMobile ? 3 : 7, padding: 10, font: {size: 12}}},
      y: {stacked: true, beginAtZero: true, max: Math.max(1, peak) * 1.08, border: {display: false, dash: [4, 4]},
        grid: {color: theme.grid, drawTicks: false},
        ticks: {color: theme.textSecondary, includeBounds: false, maxTicksLimit: 5, padding: 10, font: {size: 12}, callback: value => formatCompactNumber(Number(value))}},
    },
  }), [buckets.length, fullLabels, isMobile, theme, view.rows, peak]);

  const metricLines = (row: ComparisonRow) => [
    row.label,
    `${t('usage_stats.comparison_tokens')}: ${formatCompactNumber(row.total_tokens)} · ${formatShare(row.share)}`,
    `${t('usage_stats.comparison_requests')}: ${row.requests.toLocaleString()} · ${t('usage_stats.comparison_failures')}: ${row.failures.toLocaleString()}`,
    `${t('usage_stats.comparison_input')}: ${formatCompactNumber(row.input_tokens)} · ${t('usage_stats.comparison_output')}: ${formatCompactNumber(row.output_tokens)}`,
    `${t('usage_stats.comparison_cache_read')}: ${formatCompactNumber(row.cache_read_tokens)} · ${t('usage_stats.comparison_cache_write')}: ${formatCompactNumber(row.cache_creation_tokens)}`,
    `${t('usage_stats.comparison_reasoning')}: ${formatCompactNumber(row.reasoning_tokens)}`,
    `${t('usage_stats.comparison_cache')}: ${formatShare(row.input_tokens > 0 ? row.cache_read_tokens / row.input_tokens * 100 : null)}`,
    `${t('usage_stats.comparison_cost')}: ${formatCost(row.cost)}`,
  ];
  const selectBucket = (index: number) => {
    const chart = chartRef.current;
    if (!chart || !buckets.length) return;
    keyboardIndex.current = Math.max(0, Math.min(index, buckets.length - 1));
    const active = view.rows.flatMap((row, datasetIndex) => hidden.has(row.key) ? [] : [{datasetIndex, index: keyboardIndex.current}]);
    chart.setActiveElements(active);
    chart.tooltip?.setActiveElements(active, {x: chart.scales.x.getPixelForValue(keyboardIndex.current), y: chart.chartArea.top});
    chart.update('none');
  };
  const onChartKeyDown = (event: KeyboardEvent<HTMLCanvasElement>) => {
    if (event.key === 'Escape') {
      if (tooltipRef.current) tooltipRef.current.hidden = true;
      return;
    }
    const next = event.key === 'ArrowRight' ? keyboardIndex.current + 1 : event.key === 'ArrowLeft' ? keyboardIndex.current - 1 : event.key === 'Home' ? 0 : event.key === 'End' ? buckets.length - 1 : null;
    if (next === null) return;
    event.preventDefault();
    selectBucket(next);
  };
  const empty = loading && items.length === 0 ? <><LoadingSpinner size={18} />{t('common.loading')}</> : t('usage_stats.comparison_empty');
  return <div data-comparison={dimension} aria-busy={loading}>
    {fullLabels.length > 0 && <div className={styles.topline}><span className={styles.range}>{fullLabels[0]}{fullLabels.length > 1 ? ` – ${fullLabels[fullLabels.length - 1]}` : ''}</span></div>}
    <div className={styles.chart}>
      {view.total > 0 && buckets.length > 0 ? <Line ref={chartRef} data={data} options={options} datasetIdKey="key" role="img"
        aria-label={`${t('usage_stats.comparison_token_trend')} · ${t(`usage_stats.overview_realtime_dimension_${dimension}`)} · ${t('usage_stats.comparison_chart_keyboard')}`}
        tabIndex={0} onFocus={() => selectBucket(buckets.length - 1)} onKeyDown={onChartKeyDown}
        onBlur={() => { if (tooltipRef.current) tooltipRef.current.hidden = true; }} /> : <div className={styles.empty}>{empty}</div>}
      <div ref={tooltipRef} className={styles.tooltip} role="tooltip" hidden />
    </div>
    <div className={styles.legend}>
      {view.rows.map((row, index) => <button type="button" key={row.key} data-series-key={row.key} aria-pressed={!hidden.has(row.key)}
        onClick={() => setHidden(current => { const next = new Set(current); if (next.has(row.key)) next.delete(row.key); else next.add(row.key); return next; })}>
        <i style={{background: colorFor(row, index)}} />{row.label}
      </button>)}
    </div>
    {view.rows.length > 0 && <div className={styles.detailsContainer}><div className={styles.details} style={{'--detail-count': view.rows.length} as CSSProperties}>
      {view.rows.map((row, index) => <button type="button" key={row.key} data-comparison-entry={row.key} aria-label={metricLines(row).join(', ')}
        style={{'--series-color': colorFor(row, index)} as CSSProperties}
        onMouseEnter={event => detailsTooltip.showOnMouseEnter(metricLines(row), event.currentTarget)}
        onMouseLeave={event => detailsTooltip.hideOnMouseLeave(event.currentTarget)}
        onFocus={event => detailsTooltip.showOnFocus(metricLines(row), event.currentTarget)}
        onBlur={event => detailsTooltip.hideOnBlur(event.currentTarget)}
        onClick={event => detailsTooltip.showOnFocus(metricLines(row), event.currentTarget)}
        onKeyDown={event => { if (event.key === 'Escape') dismiss(); }}>
        <span className={styles.detailName}><i /><span>{row.label}</span></span>
        <span className={styles.detailValue}>{formatCompactNumber(row.total_tokens)}<small>{formatShare(row.share)}</small></span>
      </button>)}
    </div></div>}
    <PortalTooltip tooltip={detailsTooltip.tooltip} />
  </div>;
}
