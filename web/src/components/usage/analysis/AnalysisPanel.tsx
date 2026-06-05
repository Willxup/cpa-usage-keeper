import { useEffect, useMemo, useState, type CSSProperties, type FocusEvent, type MouseEvent } from 'react';
import { useTranslation } from 'react-i18next';
import type { Chart, ChartData, ChartOptions, Plugin, ScriptableContext, TooltipModel } from 'chart.js';
import { Bar, Doughnut, Scatter } from 'react-chartjs-2';
import type { AnalysisCompositionItem, AnalysisCostBreakdown, AnalysisHeatmapCell, AnalysisModelEfficiencyItem, AnalysisResponse, AnalysisTokenUsageBucket } from '@/lib/types';
import { calculateDisplayInputTokens, calculateDisplayOutputTokens, formatCompactNumber, formatUsd } from '@/utils/usage';
import styles from './AnalysisPanel.module.scss';

interface AnalysisPanelProps {
  analysis: AnalysisResponse | null;
  loading: boolean;
  isDark: boolean;
  isMobile: boolean;
}

type ChartRow = {
  label: string;
  input: number;
  output: number;
  rawInput: number;
  rawOutput: number;
  cached: number;
  reasoning: number;
  total: number;
  requests: number;
  cost: number;
  costAvailable: boolean;
};

type ChartTheme = {
  textPrimary: string;
  textSecondary: string;
  grid: string;
  axis: string;
  tooltipBg: string;
  tooltipBorder: string;
  tooltipBody: string;
};

type LegendItem = {
  label: string;
  color: string;
};

type GradientColor = {
  base: string;
  light: string;
};

type TokenTooltipDataset = ChartData<'bar', number[], string>['datasets'][number] & {
  tooltipData?: number[];
};
type MixedTokenChartData = ChartData<'bar', Array<number | null>, string>;
type FloatingTooltipState = {
  lines: string[];
  x: number;
  y: number;
  placement: 'above' | 'below';
};
type CostBreakdownSegmentKey = 'input' | 'output' | 'cached';
type CostBreakdownSegment = {
  key: CostBreakdownSegmentKey;
  label: string;
  value: number;
  color: string;
  tokens: number;
};
type ModelEfficiencyColor = {
  base: string;
  light: string;
  dark: string;
};

const CHART_COLORS: GradientColor[] = [
  { base: '#1d4ed8', light: '#60a5fa' },
  { base: '#ca8a04', light: '#facc15' },
  { base: '#15803d', light: '#22c55e' },
  { base: '#7e22ce', light: '#c084fc' },
  { base: '#b91c1c', light: '#ef4444' },
];
const TOKEN_COLORS = {
  input: { base: '#2563eb', light: '#93c5fd' },
  output: { base: '#16a34a', light: '#86efac' },
  cached: { base: '#d97706', light: '#fde68a' },
  reasoning: { base: '#8b5cf6', light: '#d8b4fe' },
  requests: '#ff5a40',
  cost: '#14b8a6',
};
const MODEL_EFFICIENCY_COLORS: ModelEfficiencyColor[] = [
  { base: '#5b7fb9', light: '#7898c8', dark: '#395a8d' },
  { base: '#b46f68', light: '#c68b84', dark: '#864943' },
  { base: '#6f9a7a', light: '#89b193', dark: '#4b7255' },
  { base: '#b79257', light: '#c6a66d', dark: '#86652e' },
  { base: '#8d79b5', light: '#a08cc4', dark: '#66518d' },
  { base: '#5f9aa7', light: '#7aadb8', dark: '#3e737f' },
  { base: '#b07194', light: '#c188a7', dark: '#854f6c' },
  { base: '#8c9f61', light: '#a0b374', dark: '#62733d' },
];
const COST_TOOLTIP_MAX_WIDTH = 280;
const COST_TOOLTIP_VIEWPORT_PADDING = 8;
const COST_TOOLTIP_CURSOR_OFFSET = 14;
const HEATMAP_TOOLTIP_MAX_WIDTH = 280;
const HEATMAP_TOOLTIP_VIEWPORT_PADDING = 8;
const HEATMAP_TOOLTIP_CURSOR_OFFSET = 14;
const MODEL_EFFICIENCY_TOOLTIP_ID = 'analysis-model-efficiency-tooltip';
const MODEL_EFFICIENCY_TOOLTIP_MAX_WIDTH = 320;
const MODEL_EFFICIENCY_TOOLTIP_VIEWPORT_PADDING = 8;
const MODEL_EFFICIENCY_TOOLTIP_CURSOR_OFFSET = 14;
const MODEL_EFFICIENCY_MIN_RADIUS = 5;
const MODEL_EFFICIENCY_MAX_RADIUS = 24;
const MODEL_EFFICIENCY_HOVER_RADIUS_DELTA = 4;
const MODEL_EFFICIENCY_RADIUS_EASING = 0.75;
const MODEL_EFFICIENCY_OUTLIER_RATIO = 8;
const MODEL_EFFICIENCY_AXIS_PADDING_FACTOR = 2.5;
const EMPTY_COMPOSITION_ITEMS: AnalysisCompositionItem[] = [];
type TokenLabels = {
  input: string;
  output: string;
  cached: string;
  reasoning: string;
  total: string;
  requests: string;
  cost: string;
};

const drawRequestsLineOnTopPlugin: Plugin<'bar'> = {
  id: 'analysis-requests-line-on-top',
  afterDatasetsDraw: (chart) => {
    chart.data.datasets.forEach((dataset, datasetIndex) => {
      const meta = chart.getDatasetMeta(datasetIndex);
      if (meta.type === 'line' && !meta.hidden) {
        meta.controller.draw();
      }
    });
  },
};

const getChartTheme = (isDark: boolean): ChartTheme => ({
  textPrimary: isDark ? '#f5f1e8' : '#111827',
  textSecondary: isDark ? 'rgba(255, 255, 255, 0.72)' : 'rgba(17, 24, 39, 0.72)',
  grid: isDark ? 'rgba(255, 255, 255, 0.06)' : 'rgba(17, 24, 39, 0.06)',
  axis: isDark ? 'rgba(255, 255, 255, 0.10)' : 'rgba(17, 24, 39, 0.10)',
  tooltipBg: isDark ? 'rgba(17, 24, 39, 0.94)' : 'rgba(255, 255, 255, 0.98)',
  tooltipBorder: isDark ? 'rgba(255, 255, 255, 0.10)' : 'rgba(17, 24, 39, 0.10)',
  tooltipBody: isDark ? 'rgba(255, 255, 255, 0.86)' : '#374151',
});

const toNumber = (value: unknown) => {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : 0;
};

const getDatasetLabelPrefix = (dataset: unknown): string => {
  const label = dataset && typeof dataset === 'object'
    ? (dataset as { label?: unknown }).label
    : undefined;
  return typeof label === 'string' && label ? `${label}: ` : '';
};

const getTooltipTokenValue = (dataset: unknown, dataIndex: number | undefined, fallback: unknown): number => {
  const tooltipData = dataset && typeof dataset === 'object'
    ? (dataset as { tooltipData?: unknown[] }).tooltipData
    : undefined;
  const tooltipValue = typeof dataIndex === 'number' ? tooltipData?.[dataIndex] : undefined;
  return toNumber(tooltipValue ?? fallback);
};

const createChartGradient = (ctx: CanvasRenderingContext2D, chartArea: { top: number; bottom: number }, color: GradientColor) => {
  const gradient = ctx.createLinearGradient(0, chartArea.top, 0, chartArea.bottom);
  gradient.addColorStop(0, color.light);
  gradient.addColorStop(1, color.base);
  return gradient;
};

const toGradientFill = (context: { chart: { ctx: CanvasRenderingContext2D; chartArea?: { top: number; bottom: number } } }, color: GradientColor) => {
  const { chart } = context;
  if (!chart.chartArea) return color.base;
  return createChartGradient(chart.ctx, chart.chartArea, color);
};

const formatPercent = (value: number) => `${value.toFixed(2)}%`;

const interpolateColor = (from: [number, number, number], to: [number, number, number], ratio: number) => {
  const clampedRatio = Math.max(0, Math.min(1, ratio));
  return from.map((channel, index) => Math.round(channel + (to[index] - channel) * clampedRatio));
};

const getHeatmapCellColor = (intensity: number, isDark: boolean) => {
  const clampedIntensity = Math.max(0, Math.min(1, intensity));
  const stops: Array<{ at: number; color: [number, number, number] }> = [
    ...(isDark
      ? [
        { at: 0, color: [26, 17, 24] },
        { at: 0.24, color: [74, 31, 35] },
        { at: 0.48, color: [154, 52, 18] },
        { at: 0.74, color: [249, 115, 22] },
        { at: 1, color: [253, 230, 138] },
      ] satisfies Array<{ at: number; color: [number, number, number] }>
      : [
        { at: 0, color: [255, 247, 237] },
        { at: 0.22, color: [254, 215, 170] },
        { at: 0.48, color: [251, 146, 60] },
        { at: 0.72, color: [239, 68, 68] },
        { at: 1, color: [124, 45, 18] },
      ] satisfies Array<{ at: number; color: [number, number, number] }>),
  ];
  const upperIndex = stops.findIndex((stop) => clampedIntensity <= stop.at);
  if (upperIndex <= 0) return `rgb(${stops[0].color.join(', ')})`;
  const lower = stops[upperIndex - 1];
  const upper = stops[upperIndex];
  const ratio = (clampedIntensity - lower.at) / (upper.at - lower.at);
  return `rgb(${interpolateColor(lower.color, upper.color, ratio).join(', ')})`;
};

const getHeatmapCellTextColor = (intensity: number, isDark: boolean) => {
  const clampedIntensity = Math.max(0, Math.min(1, intensity));
  if (!isDark) {
    return clampedIntensity > 0.58 ? '#fff7ed' : '#431407';
  }
  return clampedIntensity > 0.86 ? '#1c1208' : '#fff7ed';
};

const getHeatmapVisualIntensity = (value: number, maxValue: number) => {
  if (value <= 0 || maxValue <= 0) return 0;
  const rawIntensity = value / maxValue;
  return 0.05 + 0.95 * Math.pow(rawIntensity, 0.65);
};

const formatBucketLabel = (bucket: string, granularity: AnalysisResponse['granularity']) => {
  const date = new Date(bucket);
  if (Number.isNaN(date.getTime())) return bucket;
  if (granularity === 'daily') {
    return `${date.getMonth() + 1}/${date.getDate()}`;
  }
  return `${String(date.getHours()).padStart(2, '0')}:00`;
};

function buildTokenUsageRows(buckets: AnalysisTokenUsageBucket[], granularity: AnalysisResponse['granularity']): ChartRow[] {
  return buckets.map((bucket) => ({
    label: formatBucketLabel(bucket.bucket, granularity),
    input: calculateDisplayInputTokens({
      inputTokens: bucket.input_tokens,
      cachedTokens: bucket.cached_tokens,
    }),
    output: calculateDisplayOutputTokens({
      outputTokens: bucket.output_tokens,
      reasoningTokens: bucket.reasoning_tokens,
    }),
    rawInput: toNumber(bucket.input_tokens),
    rawOutput: toNumber(bucket.output_tokens),
    cached: toNumber(bucket.cached_tokens),
    reasoning: toNumber(bucket.reasoning_tokens),
    total: toNumber(bucket.total_tokens),
    requests: toNumber(bucket.requests),
    cost: toNumber(bucket.cost_usd),
    costAvailable: bucket.cost_available !== false,
  }));
}

function takeMajorComposition(items: AnalysisCompositionItem[], othersLabel: string, limit = 5): AnalysisCompositionItem[] {
  if (items.length <= limit) return items;
  const major = items.slice(0, limit);
  const rest = items.slice(limit).reduce(
    (sum, item) => ({
      total_tokens: sum.total_tokens + toNumber(item.total_tokens),
      requests: sum.requests + toNumber(item.requests),
      input_tokens: sum.input_tokens + toNumber(item.input_tokens),
      output_tokens: sum.output_tokens + toNumber(item.output_tokens),
      cached_tokens: sum.cached_tokens + toNumber(item.cached_tokens),
      reasoning_tokens: sum.reasoning_tokens + toNumber(item.reasoning_tokens),
      cost_usd: sum.cost_usd + toNumber(item.cost_usd),
      cost_available: sum.cost_available && item.cost_available !== false,
    }),
    { total_tokens: 0, requests: 0, input_tokens: 0, output_tokens: 0, cached_tokens: 0, reasoning_tokens: 0, cost_usd: 0, cost_available: true },
  );
  const total = items.reduce((sum, item) => sum + toNumber(item.total_tokens), 0);
  return [
    ...major,
    {
      key: '__others__',
      label: othersLabel,
      total_tokens: rest.total_tokens,
      requests: rest.requests,
      input_tokens: rest.input_tokens,
      output_tokens: rest.output_tokens,
      cached_tokens: rest.cached_tokens,
      reasoning_tokens: rest.reasoning_tokens,
      cost_usd: rest.cost_usd,
      cost_available: rest.cost_available,
      percent: total > 0 ? (rest.total_tokens / total) * 100 : 0,
    },
  ];
}

function buildTokenLegendItems(labels: TokenLabels): LegendItem[] {
  return [
    { label: labels.input, color: TOKEN_COLORS.input.base },
    { label: labels.output, color: TOKEN_COLORS.output.base },
    { label: labels.cached, color: TOKEN_COLORS.cached.base },
    { label: labels.reasoning, color: TOKEN_COLORS.reasoning.base },
    { label: labels.requests, color: TOKEN_COLORS.requests },
    { label: labels.cost, color: TOKEN_COLORS.cost },
  ];
}

function buildAnalysisTokenChartOptions({ chartTheme, isMobile, totalTokens, totalLabel }: { chartTheme: ChartTheme; isMobile: boolean; totalTokens: number[]; totalLabel: string }): ChartOptions<'bar'> {
  return {
    responsive: true,
    maintainAspectRatio: false,
    interaction: { mode: 'index', intersect: false },
    plugins: {
      legend: { display: false },
      tooltip: {
        backgroundColor: chartTheme.tooltipBg,
        titleColor: chartTheme.textPrimary,
        bodyColor: chartTheme.tooltipBody,
        borderColor: chartTheme.tooltipBorder,
        borderWidth: 1,
        padding: 10,
        displayColors: true,
        usePointStyle: true,
        callbacks: {
          label: (context) => {
            const label = getDatasetLabelPrefix(context.dataset);
            const value = getTooltipTokenValue(context.dataset, context.dataIndex, context.parsed.y);
            const axisID = context.dataset && typeof context.dataset === 'object'
              ? (context.dataset as { yAxisID?: unknown }).yAxisID
              : undefined;
            return `${label}${axisID === 'cost' ? formatUsd(value) : formatCompactNumber(value)}`;
          },
          footer: (items) => {
            const dataIndex = items[0]?.dataIndex ?? -1;
            if (dataIndex < 0) return '';
            return `${totalLabel}: ${formatCompactNumber(Number(totalTokens[dataIndex] ?? 0))}`;
          },
        },
      },
    },
    scales: {
      x: {
        stacked: true,
        grid: { color: chartTheme.grid, drawTicks: false },
        border: { color: chartTheme.axis },
        ticks: { color: chartTheme.textSecondary, font: { size: 10 }, maxRotation: isMobile ? 0 : 45, autoSkip: true, maxTicksLimit: isMobile ? 8 : 12 },
      },
      tokens: {
        type: 'linear',
        position: 'left',
        stacked: true,
        beginAtZero: true,
        grid: { color: chartTheme.grid },
        border: { color: chartTheme.axis },
        ticks: { color: chartTheme.textSecondary, font: { size: 10 }, maxTicksLimit: 5, callback: (value) => formatCompactNumber(Number(value)) },
      },
      requests: {
        type: 'linear',
        position: 'right',
        beginAtZero: true,
        grid: { drawOnChartArea: false },
        border: { color: chartTheme.axis },
        ticks: { color: chartTheme.textSecondary, font: { size: 10 }, maxTicksLimit: 4, callback: (value) => formatCompactNumber(Number(value)) },
      },
      cost: {
        type: 'linear',
        position: 'right',
        beginAtZero: true,
        grid: { drawOnChartArea: false },
        border: { color: chartTheme.axis },
        ticks: { color: chartTheme.textSecondary, font: { size: 10 }, maxTicksLimit: 4, callback: (value) => formatUsd(Number(value)) },
      },
    },
  };
}

function buildAnalysisTokenChartData(rows: ChartRow[], labels: TokenLabels): MixedTokenChartData {
  const tokenColors = TOKEN_COLORS;
  return {
    labels: rows.map((row) => row.label),
    datasets: [
      { label: labels.input, data: rows.map((row) => row.input), tooltipData: rows.map((row) => row.rawInput), backgroundColor: (context) => toGradientFill(context, tokenColors.input), borderColor: tokenColors.input.base, stack: 'tokens', yAxisID: 'tokens' } as TokenTooltipDataset,
      { label: labels.output, data: rows.map((row) => row.output), tooltipData: rows.map((row) => row.rawOutput), backgroundColor: (context) => toGradientFill(context, tokenColors.output), borderColor: tokenColors.output.base, stack: 'tokens', yAxisID: 'tokens' } as TokenTooltipDataset,
      { label: labels.cached, data: rows.map((row) => row.cached), tooltipData: rows.map((row) => row.cached), backgroundColor: (context) => toGradientFill(context, tokenColors.cached), borderColor: tokenColors.cached.base, stack: 'tokens', yAxisID: 'tokens' } as TokenTooltipDataset,
      { label: labels.reasoning, data: rows.map((row) => row.reasoning), tooltipData: rows.map((row) => row.reasoning), backgroundColor: (context) => toGradientFill(context, tokenColors.reasoning), borderColor: tokenColors.reasoning.base, stack: 'tokens', yAxisID: 'tokens' } as TokenTooltipDataset,
      {
        type: 'line',
        label: labels.requests,
        data: rows.map((row) => row.requests),
        borderColor: tokenColors.requests,
        backgroundColor: tokenColors.requests,
        pointBackgroundColor: tokenColors.requests,
        pointBorderColor: tokenColors.requests,
        tension: 0.35,
        borderWidth: 2,
        borderDash: [6, 4],
        pointRadius: 0,
        yAxisID: 'requests',
      } as unknown as MixedTokenChartData['datasets'][number],
      {
        type: 'line',
        label: labels.cost,
        data: rows.map((row) => (row.costAvailable ? row.cost : null)),
        borderColor: tokenColors.cost,
        backgroundColor: tokenColors.cost,
        pointBackgroundColor: tokenColors.cost,
        pointBorderColor: tokenColors.cost,
        tension: 0.35,
        borderWidth: 2,
        pointRadius: 2,
        yAxisID: 'cost',
      } as unknown as MixedTokenChartData['datasets'][number],
    ],
  };
}

function buildCompositionChartData(items: AnalysisCompositionItem[]): ChartData<'doughnut', number[], string> {
  return {
    labels: items.map((item) => item.label),
    datasets: [{
      data: items.map((item) => toNumber(item.total_tokens)),
      backgroundColor: (context) => toGradientFill(context, CHART_COLORS[context.dataIndex % CHART_COLORS.length]),
      borderColor: 'transparent',
      borderWidth: 0,
    }],
  };
}

function buildCompositionChartOptions(chartTheme: ChartTheme): ChartOptions<'doughnut'> {
  return {
    responsive: true,
    maintainAspectRatio: false,
    cutout: '58%',
    plugins: {
      legend: { display: false },
      tooltip: {
        backgroundColor: chartTheme.tooltipBg,
        titleColor: chartTheme.textPrimary,
        bodyColor: chartTheme.tooltipBody,
        borderColor: chartTheme.tooltipBorder,
        borderWidth: 1,
        padding: 10,
        displayColors: true,
        usePointStyle: true,
        callbacks: {
          label: (context) => formatCompactNumber(Number(context.parsed ?? 0)),
        },
      },
    },
  };
}

function TokenUsageChart({ rows, loading, isDark, isMobile }: { rows: ChartRow[]; loading: boolean; isDark: boolean; isMobile: boolean }) {
  const { t } = useTranslation();
  const tokenLabels = useMemo(() => ({
    input: t('usage_stats.input_tokens'),
    output: t('usage_stats.output_tokens'),
    cached: t('usage_stats.cached_tokens'),
    reasoning: t('usage_stats.reasoning_tokens'),
    total: t('usage_stats.total_tokens'),
    requests: t('usage_stats.requests_count'),
    cost: t('usage_stats.total_cost'),
  }), [t]);
  const chartTheme = useMemo(() => getChartTheme(isDark), [isDark]);
  const chartData = useMemo(() => buildAnalysisTokenChartData(rows, tokenLabels), [rows, tokenLabels]);
  const chartOptions = useMemo(() => buildAnalysisTokenChartOptions({
    chartTheme,
    isMobile,
    totalTokens: rows.map((row) => row.total),
    totalLabel: tokenLabels.total,
  }), [chartTheme, isMobile, rows, tokenLabels.total]);
  const legendItems = useMemo(() => buildTokenLegendItems(tokenLabels), [tokenLabels]);
  return (
    <section className={`${styles.analysisCard} ${styles.tokenUsageCard}`}>
      <div className={styles.cardHeader}>
        <div>
          <h2>{t('usage_stats.analysis_token_usage_title')}</h2>
          <p>{t('usage_stats.analysis_token_usage_subtitle')}</p>
        </div>
      </div>
      {loading ? (
        <div className={styles.emptyState}>{t('common.loading')}</div>
      ) : rows.length === 0 ? (
        <div className={styles.emptyState}>{t('usage_stats.no_data')}</div>
      ) : (
        <div className={styles.analysisChartSurface}>
          <div className={styles.analysisChartLegend} aria-label="Token chart legend">
            {legendItems.map((item) => (
              <div key={item.label} className={styles.analysisLegendItem} title={item.label}>
                <span className={styles.analysisLegendDot} style={{ backgroundColor: item.color }} />
                <span className={styles.analysisLegendLabel}>{item.label}</span>
              </div>
            ))}
          </div>
          <div className={styles.tokenChartFrame}>
            <Bar data={chartData} options={chartOptions} plugins={[drawRequestsLineOnTopPlugin]} />
          </div>
        </div>
      )}
    </section>
  );
}

type CompositionTab = {
  id: 'api_key' | 'model' | 'auth_files' | 'ai_provider';
  label: string;
  items: AnalysisCompositionItem[];
};

function CompositionPanel({ tabs, loading, isDark }: { tabs: CompositionTab[]; loading: boolean; isDark: boolean }) {
  const { t } = useTranslation();
  const [activeTabId, setActiveTabId] = useState<CompositionTab['id']>('api_key');
  const activeTab = tabs.find((tab) => tab.id === activeTabId) ?? tabs[0];
  const items = activeTab?.items ?? EMPTY_COMPOSITION_ITEMS;
  const activeContentKey = `${activeTab?.id ?? 'empty'}:${items.map((item) => item.key).join('|')}`;
  const chartTheme = useMemo(() => getChartTheme(isDark), [isDark]);
  const chartData = useMemo(() => buildCompositionChartData(items), [items]);
  const chartOptions = useMemo(() => buildCompositionChartOptions(chartTheme), [chartTheme]);
  return (
    <section className={`${styles.analysisCard} ${styles.compositionCard}`}>
      <div className={styles.cardHeader}>
        <div>
          <h2>{t('usage_stats.analysis_composition_title')}</h2>
          <p>{t('usage_stats.analysis_composition_subtitle')}</p>
        </div>
      </div>
      <div className={styles.compositionTabs} role="tablist" aria-label={t('usage_stats.analysis_composition_title')}>
        {tabs.map((tab) => (
          <button
            key={tab.id}
            type="button"
            role="tab"
            aria-selected={tab.id === activeTabId}
            className={`${styles.compositionTab} ${tab.id === activeTabId ? styles.compositionTabActive : ''}`}
            onClick={() => setActiveTabId(tab.id)}
          >
            {tab.label}
          </button>
        ))}
      </div>
      {loading ? (
        <div className={styles.emptyState}>{t('common.loading')}</div>
      ) : items.length === 0 ? (
        <div className={styles.emptyState}>{t('usage_stats.no_data')}</div>
      ) : (
        <div key={activeContentKey} className={styles.analysisChartSurface}>
          <div className={styles.compositionLayout}>
            <div className={styles.donutChartFrame}>
              <Doughnut key={`chart-${activeContentKey}`} data={chartData} options={chartOptions} />
            </div>
            <div className={styles.compositionTableWrap}>
              <table key={`table-${activeContentKey}`} className={styles.compositionTable}>
                <thead>
                  <tr>
                    <th>{t('usage_stats.analysis_composition_name')}</th>
                    <th>{t('usage_stats.total_tokens')}</th>
                    <th>{t('usage_stats.analysis_composition_token_percent')}</th>
                    <th>{t('usage_stats.total_cost')}</th>
                    <th>{t('usage_stats.requests_count')}</th>
                  </tr>
                </thead>
                <tbody>
                  {items.map((item, index) => (
                    <tr key={`${activeTab.id}-${item.key}`}>
                      <td>
                        <span className={styles.legendDot} style={{ backgroundColor: CHART_COLORS[index % CHART_COLORS.length].base }} />
                        <span className={styles.compositionName}>{item.label}</span>
                      </td>
                      <td>{formatCompactNumber(toNumber(item.total_tokens))}</td>
                      <td>{formatPercent(toNumber(item.percent))}</td>
                      <td>{item.cost_available === false ? t('usage_stats.cost_need_price') : formatUsd(toNumber(item.cost_usd))}</td>
                      <td>{formatCompactNumber(toNumber(item.requests))}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </div>
      )}
    </section>
  );
}

function getCostRatePerMillion(cost: number, tokens: number) {
  return tokens > 0 ? (cost / tokens) * 1_000_000 : 0;
}

function getCostSegmentTokens(rows: ChartRow[]): Record<CostBreakdownSegmentKey, number> {
  return rows.reduce(
    (totals, row) => ({
      input: totals.input + Math.max(row.rawInput - row.cached, 0),
      output: totals.output + row.rawOutput,
      cached: totals.cached + row.cached,
    }),
    { input: 0, output: 0, cached: 0 },
  );
}

function CostBreakdownCard({ breakdown, rows, loading }: { breakdown: AnalysisCostBreakdown | undefined; rows: ChartRow[]; loading: boolean }) {
  const { t } = useTranslation();
  const [costTooltip, setCostTooltip] = useState<FloatingTooltipState | null>(null);
  const safeBreakdown = breakdown ?? { input_cost_usd: 0, output_cost_usd: 0, cached_cost_usd: 0, total_cost_usd: 0, cost_available: true };
  const totalCost = toNumber(safeBreakdown.total_cost_usd);
  const totalTokens = rows.reduce((sum, row) => sum + row.total, 0);
  const segmentTokens = getCostSegmentTokens(rows);
  const costAvailable = safeBreakdown.cost_available !== false;
  const blendedRate = costAvailable ? getCostRatePerMillion(totalCost, totalTokens) : null;
  const ratePoints = costAvailable
    ? rows
      .filter((row) => row.costAvailable && row.total > 0)
      .map((row) => getCostRatePerMillion(row.cost, row.total))
    : [];
  const rateMax = Math.max(0, ...ratePoints);
  const segments: CostBreakdownSegment[] = [
    { key: 'input', label: t('usage_stats.input_tokens'), value: toNumber(safeBreakdown.input_cost_usd), color: TOKEN_COLORS.input.base, tokens: segmentTokens.input },
    { key: 'output', label: t('usage_stats.output_tokens'), value: toNumber(safeBreakdown.output_cost_usd), color: TOKEN_COLORS.output.base, tokens: segmentTokens.output },
    { key: 'cached', label: t('usage_stats.cached_tokens'), value: toNumber(safeBreakdown.cached_cost_usd), color: TOKEN_COLORS.cached.base, tokens: segmentTokens.cached },
  ];
  const hasData = totalCost > 0 || segments.some((segment) => segment.value > 0);
  const buildCostTooltipLines = (segment: CostBreakdownSegment, percent: number) => [
    segment.label,
    `${t('usage_stats.total_cost')}: ${formatUsd(segment.value)}`,
    `${t('usage_stats.analysis_cost_share')}: ${formatPercent(percent)}`,
    `${t('usage_stats.total_tokens')}: ${formatCompactNumber(segment.tokens)}`,
    `${t('usage_stats.analysis_cost_per_million_tokens')}: ${formatUsd(getCostRatePerMillion(segment.value, segment.tokens))}`,
  ];
  const showCostTooltip = (
    lines: string[],
    event: MouseEvent<HTMLSpanElement> | FocusEvent<HTMLSpanElement>,
  ) => {
    const viewportWidth = typeof window === 'undefined' ? 1024 : window.innerWidth;
    const viewportHeight = typeof window === 'undefined' ? 768 : window.innerHeight;
    const rect = event.currentTarget.getBoundingClientRect();
    const pointerX = 'clientX' in event && event.clientX > 0 ? event.clientX : rect.left + rect.width / 2;
    const pointerY = 'clientY' in event && event.clientY > 0 ? event.clientY : rect.top + rect.height / 2;
    const left = Math.max(
      COST_TOOLTIP_VIEWPORT_PADDING,
      Math.min(pointerX + COST_TOOLTIP_CURSOR_OFFSET, viewportWidth - COST_TOOLTIP_MAX_WIDTH - COST_TOOLTIP_VIEWPORT_PADDING),
    );
    const placement = pointerY > viewportHeight - 200 ? 'above' : 'below';
    const y = pointerY + (placement === 'above' ? -COST_TOOLTIP_CURSOR_OFFSET : COST_TOOLTIP_CURSOR_OFFSET);
    setCostTooltip({ lines, x: left, y, placement });
  };
  const hideCostTooltip = () => setCostTooltip(null);
  return (
    <section className={`${styles.analysisCard} ${styles.costBreakdownCard}`}>
      <div className={styles.cardHeader}>
        <div>
          <h2>{t('usage_stats.analysis_cost_breakdown_title')}</h2>
          <p>{t('usage_stats.analysis_cost_breakdown_subtitle')}</p>
        </div>
      </div>
      {loading ? (
        <div className={styles.emptyState}>{t('common.loading')}</div>
      ) : !hasData ? (
        <div className={styles.emptyState}>{safeBreakdown.cost_available === false ? t('usage_stats.cost_need_price') : t('usage_stats.no_data')}</div>
      ) : (
        <div className={styles.costBreakdownBody}>
          {safeBreakdown.cost_available === false ? <div className={styles.costWarning}>{t('usage_stats.cost_need_price')}</div> : null}
          <div className={styles.costStack} aria-label={t('usage_stats.analysis_cost_breakdown_title')}>
            {segments.map((segment) => {
              const percent = totalCost > 0 ? (segment.value / totalCost) * 100 : 0;
              const tooltipLines = buildCostTooltipLines(segment, percent);
              return (
                <span
                  key={segment.key}
                  className={styles.costStackSegment}
                  style={{
                    '--cost-segment-color': segment.color,
                    flexBasis: `${Math.max(percent, segment.value > 0 ? 4 : 0)}%`,
                  } as CSSProperties}
                  tabIndex={0}
                  aria-label={tooltipLines.join(', ')}
                  onMouseEnter={(event) => showCostTooltip(tooltipLines, event)}
                  onMouseMove={(event) => showCostTooltip(tooltipLines, event)}
                  onMouseLeave={hideCostTooltip}
                  onFocus={(event) => showCostTooltip(tooltipLines, event)}
                  onBlur={hideCostTooltip}
                >
                  <span>{formatPercent(percent)}</span>
                </span>
              );
            })}
          </div>
          {costTooltip ? (
            <div
              className={styles.costStackFloatingTooltip}
              role="tooltip"
              style={{
                left: costTooltip.x,
                top: costTooltip.y,
                transform: costTooltip.placement === 'above' ? 'translateY(-100%)' : undefined,
              }}
            >
              {costTooltip.lines.map((line, index) => (
                <span key={`${index}-${line}`} className={index === 0 ? styles.costStackTooltipTitle : ''}>{line}</span>
              ))}
            </div>
          ) : null}
          <div className={styles.costRatePanel}>
            <div className={styles.costRateMetric}>
              <span>{t('usage_stats.total_cost')}</span>
              <strong>{safeBreakdown.cost_available === false ? t('usage_stats.cost_need_price') : formatUsd(totalCost)}</strong>
            </div>
            <div className={styles.costRateMetric}>
              <span>{t('usage_stats.analysis_cost_per_million_tokens')}</span>
              <strong>{blendedRate === null ? t('usage_stats.cost_need_price') : formatUsd(blendedRate)}</strong>
              <small>{t('usage_stats.analysis_blended_rate')}</small>
            </div>
            <div className={styles.costRateSparkline} aria-label={t('usage_stats.analysis_cost_per_million_tokens')}>
              {ratePoints.length === 0 ? (
                <span className={styles.costRateSparkEmpty} />
              ) : ratePoints.slice(-12).map((point, index) => (
                <span
                  key={`${index}-${point}`}
                  className={styles.costRateSparkBar}
                  style={{ height: `${Math.max(12, rateMax > 0 ? (point / rateMax) * 100 : 0)}%` }}
                  title={formatUsd(point)}
                />
              ))}
            </div>
          </div>
          <div className={styles.costMetricGrid}>
            {segments.map((segment) => (
              <div key={segment.key} className={styles.costMetric}>
                <span className={styles.costMetricDot} style={{ backgroundColor: segment.color }} />
                <span className={styles.costMetricLabel}>{segment.label}</span>
                <strong>{formatUsd(segment.value)}</strong>
                <small>{formatPercent(totalCost > 0 ? (segment.value / totalCost) * 100 : 0)}</small>
              </div>
            ))}
          </div>
        </div>
      )}
    </section>
  );
}

type EfficiencyPoint = {
  x: number;
  y: number;
  model: string;
  requests: number;
  cost: number;
  totalTokens: number;
  cacheRate: number;
};

const getEfficiencyPalette = (index: number) => {
  return MODEL_EFFICIENCY_COLORS[index % MODEL_EFFICIENCY_COLORS.length];
};

const getEfficiencyColor = (index: number) => getEfficiencyPalette(index).base;

const getNearestRankPercentile = (values: number[], percentile: number) => {
  const sortedValues = values
    .filter((value) => Number.isFinite(value) && value > 0)
    .sort((a, b) => a - b);
  if (sortedValues.length === 0) return 0;
  const index = Math.min(sortedValues.length - 1, Math.max(0, Math.ceil(percentile * sortedValues.length) - 1));
  return sortedValues[index];
};

const buildModelEfficiencyRadii = (values: number[]) => {
  const positiveValues = values.filter((value) => Number.isFinite(value) && value > 0);
  if (positiveValues.length === 0) {
    return values.map(() => MODEL_EFFICIENCY_MIN_RADIUS);
  }
  const minValue = Math.min(...positiveValues);
  const maxValue = Math.max(...positiveValues);
  if (minValue === maxValue) {
    const radius = (MODEL_EFFICIENCY_MIN_RADIUS + MODEL_EFFICIENCY_MAX_RADIUS) / 2;
    return values.map((value) => (value > 0 ? radius : MODEL_EFFICIENCY_MIN_RADIUS));
  }

  // 用 log 压缩头部模型，并在明显离群时把参考上限拉回到头部和长尾之间。
  const p90Value = getNearestRankPercentile(positiveValues, 0.9);
  const referenceMax = p90Value > 0 && maxValue > p90Value * MODEL_EFFICIENCY_OUTLIER_RATIO
    ? Math.sqrt(maxValue * p90Value)
    : maxValue;
  const logMin = Math.log(minValue + 1);
  const logMax = Math.log(Math.max(referenceMax, minValue * 1.1) + 1);
  const logRange = Math.max(logMax - logMin, Number.EPSILON);
  return values.map((value) => {
    if (!Number.isFinite(value) || value <= 0) return MODEL_EFFICIENCY_MIN_RADIUS;
    const clampedValue = Math.min(value, referenceMax);
    const normalized = Math.max(0, Math.min(1, (Math.log(clampedValue + 1) - logMin) / logRange));
    const eased = Math.pow(normalized, MODEL_EFFICIENCY_RADIUS_EASING);
    const radius = MODEL_EFFICIENCY_MIN_RADIUS + eased * (MODEL_EFFICIENCY_MAX_RADIUS - MODEL_EFFICIENCY_MIN_RADIUS);
    return Number(radius.toFixed(2));
  });
};

const getLogScaleBounds = (values: number[]) => {
  const positiveValues = values.filter((value) => Number.isFinite(value) && value > 0);
  if (positiveValues.length === 0) return {};
  const minValue = Math.min(...positiveValues);
  const maxValue = Math.max(...positiveValues);
  return {
    min: Math.max(minValue / MODEL_EFFICIENCY_AXIS_PADDING_FACTOR, Number.EPSILON),
    max: maxValue * MODEL_EFFICIENCY_AXIS_PADDING_FACTOR,
  };
};

type ModelEfficiencyPointContext = ScriptableContext<'line'> & {
  element?: {
    x?: number;
    y?: number;
    options?: {
      radius?: number;
    };
  };
};

const getEfficiencyPointFill = (context: ModelEfficiencyPointContext) => {
  const palette = getEfficiencyPalette(context.dataIndex ?? 0);
  const { ctx } = context.chart;
  const x = context.element?.x;
  const y = context.element?.y;
  if (typeof x !== 'number' || typeof y !== 'number') {
    return palette.base;
  }
  const radius = typeof context.element?.options?.radius === 'number' ? context.element.options.radius : 12;
  const gradient = ctx.createLinearGradient(x - radius, y, x + radius, y);
  gradient.addColorStop(0, palette.light);
  gradient.addColorStop(1, palette.base);
  return gradient;
};

const getModelEfficiencyRate = (row: AnalysisModelEfficiencyItem) => {
  return getCostRatePerMillion(toNumber(row.cost_usd), toNumber(row.total_tokens));
};

type ModelEfficiencyTooltipLabels = {
  totalTokens: string;
  costPerMillion: string;
  requests: string;
};

function getModelEfficiencyTooltipElement() {
  let tooltipEl = document.getElementById(MODEL_EFFICIENCY_TOOLTIP_ID) as HTMLDivElement | null;
  if (tooltipEl) return tooltipEl;
  tooltipEl = document.createElement('div');
  tooltipEl.id = MODEL_EFFICIENCY_TOOLTIP_ID;
  tooltipEl.className = styles.modelEfficiencyFloatingTooltip;
  document.body.appendChild(tooltipEl);
  return tooltipEl;
}

function removeModelEfficiencyTooltip() {
  document.getElementById(MODEL_EFFICIENCY_TOOLTIP_ID)?.remove();
}

function appendModelEfficiencyTooltipMetric(group: HTMLDivElement, label: string, value: string) {
  const metric = document.createElement('div');
  metric.className = styles.modelEfficiencyTooltipMetric;
  metric.textContent = `${label}: ${value}`;
  group.appendChild(metric);
}

function createModelEfficiencyTooltipHandler({
  rows,
  labels,
}: {
  rows: AnalysisModelEfficiencyItem[];
  labels: ModelEfficiencyTooltipLabels;
}): (args: { chart: Chart; tooltip: TooltipModel<'scatter'> }) => void {
  return ({ chart, tooltip }) => {
    if (typeof document === 'undefined') return;
    const tooltipEl = getModelEfficiencyTooltipElement();
    if (tooltip.opacity === 0) {
      tooltipEl.style.opacity = '0';
      return;
    }

    tooltipEl.replaceChildren();
    const renderedIndexes = new Set<number>();
    for (const dataPoint of tooltip.dataPoints ?? []) {
      if (renderedIndexes.has(dataPoint.dataIndex)) continue;
      renderedIndexes.add(dataPoint.dataIndex);
      const row = rows[dataPoint.dataIndex];
      if (!row) continue;

      const group = document.createElement('div');
      group.className = styles.modelEfficiencyTooltipGroup;

      const header = document.createElement('div');
      header.className = styles.modelEfficiencyTooltipHeader;
      const dot = document.createElement('span');
      dot.className = styles.modelEfficiencyTooltipDot;
      dot.style.background = getEfficiencyColor(dataPoint.dataIndex);
      header.appendChild(dot);
      const name = document.createElement('strong');
      name.textContent = row.model;
      header.appendChild(name);
      group.appendChild(header);

      appendModelEfficiencyTooltipMetric(group, labels.totalTokens, formatCompactNumber(toNumber(row.total_tokens)));
      appendModelEfficiencyTooltipMetric(group, labels.costPerMillion, formatUsd(getModelEfficiencyRate(row)));
      appendModelEfficiencyTooltipMetric(group, labels.requests, formatCompactNumber(toNumber(row.requests)));
      tooltipEl.appendChild(group);
    }

    const viewportWidth = typeof window === 'undefined' ? 1024 : window.innerWidth;
    const maxWidth = Math.min(MODEL_EFFICIENCY_TOOLTIP_MAX_WIDTH, viewportWidth - MODEL_EFFICIENCY_TOOLTIP_VIEWPORT_PADDING * 2);
    tooltipEl.style.opacity = '1';
    tooltipEl.style.maxWidth = `${maxWidth}px`;
    const canvasRect = chart.canvas.getBoundingClientRect();
    const tooltipWidth = tooltipEl.offsetWidth || MODEL_EFFICIENCY_TOOLTIP_MAX_WIDTH;
    const tooltipHeight = tooltipEl.offsetHeight || 160;
    const rawLeft = canvasRect.left + tooltip.caretX + MODEL_EFFICIENCY_TOOLTIP_CURSOR_OFFSET;
    const left = Math.max(MODEL_EFFICIENCY_TOOLTIP_VIEWPORT_PADDING, Math.min(rawLeft, viewportWidth - tooltipWidth - MODEL_EFFICIENCY_TOOLTIP_VIEWPORT_PADDING));
    const rawTop = canvasRect.top + tooltip.caretY - tooltipHeight / 2;
    const top = Math.max(MODEL_EFFICIENCY_TOOLTIP_VIEWPORT_PADDING, rawTop);
    tooltipEl.style.left = `${left}px`;
    tooltipEl.style.top = `${top}px`;
  };
}

function ModelEfficiencyCard({ rows, loading, isDark, isMobile }: { rows: AnalysisModelEfficiencyItem[]; loading: boolean; isDark: boolean; isMobile: boolean }) {
  const { t } = useTranslation();
  const chartTheme = useMemo(() => getChartTheme(isDark), [isDark]);
  const pricedRows = useMemo(() => rows.filter((row) => row.cost_available !== false && toNumber(row.total_tokens) > 0 && getModelEfficiencyRate(row) > 0), [rows]);
  const tooltipLabels = useMemo(() => ({
    totalTokens: t('usage_stats.total_tokens'),
    costPerMillion: t('usage_stats.analysis_cost_per_million_tokens'),
    requests: t('usage_stats.requests_count'),
  }), [t]);
  const pointRadii = useMemo(() => buildModelEfficiencyRadii(pricedRows.map((row) => toNumber(row.requests))), [pricedRows]);
  const chartData = useMemo<ChartData<'scatter', EfficiencyPoint[], string>>(() => ({
    labels: pricedRows.map((row) => row.model),
    datasets: [{
      label: t('usage_stats.analysis_model_efficiency_title'),
      data: pricedRows.map((row) => ({
        x: toNumber(row.total_tokens),
        y: getModelEfficiencyRate(row),
        model: row.model,
        requests: toNumber(row.requests),
        cost: toNumber(row.cost_usd),
        totalTokens: toNumber(row.total_tokens),
        cacheRate: toNumber(row.cache_rate),
      })),
      pointRadius: pointRadii,
      pointHoverRadius: pointRadii.map((radius) => Math.min(MODEL_EFFICIENCY_MAX_RADIUS + MODEL_EFFICIENCY_HOVER_RADIUS_DELTA, radius + MODEL_EFFICIENCY_HOVER_RADIUS_DELTA)),
      backgroundColor: getEfficiencyPointFill,
      borderColor: pricedRows.map((_, index) => getEfficiencyPalette(index).base),
      borderWidth: 1,
      clip: false,
    }],
  }), [pointRadii, pricedRows, t]);
  const chartOptions = useMemo<ChartOptions<'scatter'>>(() => ({
    responsive: true,
    maintainAspectRatio: false,
    layout: { padding: { top: 16, right: 24, bottom: 22, left: 18 } },
    plugins: {
      legend: { display: false },
      tooltip: {
        enabled: false,
        external: createModelEfficiencyTooltipHandler({ rows: pricedRows, labels: tooltipLabels }),
        backgroundColor: chartTheme.tooltipBg,
        titleColor: chartTheme.textPrimary,
        bodyColor: chartTheme.tooltipBody,
        borderColor: chartTheme.tooltipBorder,
        borderWidth: 1,
        callbacks: {
          title: () => [],
          label: (context) => {
            const row = pricedRows[context.dataIndex];
            if (!row) return '';
            return [
              row.model,
              `${t('usage_stats.total_tokens')}: ${formatCompactNumber(row.total_tokens)}`,
              `${t('usage_stats.analysis_cost_per_million_tokens')}: ${formatUsd(getModelEfficiencyRate(row))}`,
              `${t('usage_stats.requests_count')}: ${formatCompactNumber(row.requests)}`,
            ];
          },
        },
      },
    },
    scales: {
      x: {
        type: 'logarithmic',
        ...getLogScaleBounds(pricedRows.map((row) => toNumber(row.total_tokens))),
        grid: { color: chartTheme.grid },
        border: { color: chartTheme.axis },
        ticks: { color: chartTheme.textSecondary, font: { size: 10 }, maxTicksLimit: isMobile ? 4 : 5, callback: (value) => formatCompactNumber(Number(value)) },
      },
      y: {
        type: 'logarithmic',
        ...getLogScaleBounds(pricedRows.map((row) => getModelEfficiencyRate(row))),
        grid: { color: chartTheme.grid },
        border: { color: chartTheme.axis },
        ticks: { color: chartTheme.textSecondary, font: { size: 10 }, maxTicksLimit: isMobile ? 4 : 5, callback: (value) => formatUsd(Number(value)) },
      },
    },
  }), [chartTheme, isMobile, pricedRows, t, tooltipLabels]);
  useEffect(() => {
    removeModelEfficiencyTooltip();
  }, [pricedRows]);
  useEffect(() => () => {
    removeModelEfficiencyTooltip();
  }, []);
  const hasData = rows.length > 0;
  const hasPricedData = pricedRows.length > 0;
  const hasUnavailableCost = rows.some((row) => row.cost_available === false);
  return (
    <section className={`${styles.analysisCard} ${styles.modelEfficiencyCard}`}>
      <div className={styles.cardHeader}>
        <div>
          <h2>{t('usage_stats.analysis_model_efficiency_title')}</h2>
          <p>{t('usage_stats.analysis_model_efficiency_subtitle')}</p>
        </div>
      </div>
      {loading ? (
        <div className={styles.emptyState}>{t('common.loading')}</div>
      ) : !hasData ? (
        <div className={styles.emptyState}>{t('usage_stats.no_data')}</div>
      ) : (
        <div className={styles.modelEfficiencyBody}>
          {hasUnavailableCost ? <div className={styles.costWarning}>{t('usage_stats.cost_need_price')}</div> : null}
          {hasPricedData ? (
            <div className={styles.efficiencyChartFrame}>
              <Scatter data={chartData} options={chartOptions} />
            </div>
          ) : (
            <div className={styles.emptyState}>{t('usage_stats.cost_need_price')}</div>
          )}
        </div>
      )}
    </section>
  );
}

function Heatmap({ cells, apiKeys, apiKeyLabels, models, loading, isDark }: { cells: AnalysisHeatmapCell[]; apiKeys: string[]; apiKeyLabels: Record<string, string>; models: string[]; loading: boolean; isDark: boolean }) {
  const { t } = useTranslation();
  const [tooltip, setTooltip] = useState<FloatingTooltipState | null>(null);
  const cellMap = useMemo(() => new Map(cells.map((cell) => [`${cell.api_key}\0${cell.model}`, cell])), [cells]);
  const maxHeatmapTokens = useMemo(
    () => cells.reduce((max, cell) => Math.max(max, toNumber(cell.total_tokens)), 0),
    [cells],
  );
  const getAPIKeyLabel = (apiKey: string) => apiKeyLabels[apiKey] || apiKey;
  const buildTooltipLines = (apiKey: string, model: string, cell: AnalysisHeatmapCell | undefined) => {
    const requests = toNumber(cell?.requests);
    const input = toNumber(cell?.input_tokens);
    const output = toNumber(cell?.output_tokens);
    const reasoning = toNumber(cell?.reasoning_tokens);
    const cached = toNumber(cell?.cached_tokens);
    const total = toNumber(cell?.total_tokens);
    const cost = toNumber(cell?.cost_usd);
    return [
      `${getAPIKeyLabel(apiKey)} / ${model}`,
      `${t('usage_stats.requests_count')}: ${formatCompactNumber(requests)}`,
      `${t('usage_stats.input_tokens')}: ${formatCompactNumber(input)}`,
      `${t('usage_stats.output_tokens')}: ${formatCompactNumber(output)}`,
      `${t('usage_stats.reasoning_tokens')}: ${formatCompactNumber(reasoning)}`,
      `${t('usage_stats.cached_tokens')}: ${formatCompactNumber(cached)}`,
      `${t('usage_stats.total_tokens')}: ${formatCompactNumber(total)}`,
      `${t('usage_stats.total_cost')}: ${cell?.cost_available === false ? t('usage_stats.cost_need_price') : formatUsd(cost)}`,
    ];
  };
  const showTooltip = (
    lines: string[],
    event: MouseEvent<HTMLDivElement> | FocusEvent<HTMLDivElement>,
  ) => {
    const viewportWidth = typeof window === 'undefined' ? 1024 : window.innerWidth;
    const viewportHeight = typeof window === 'undefined' ? 768 : window.innerHeight;
    const rect = event.currentTarget.getBoundingClientRect();
    const pointerX = 'clientX' in event && event.clientX > 0 ? event.clientX : rect.left + rect.width / 2;
    const pointerY = 'clientY' in event && event.clientY > 0 ? event.clientY : rect.top + rect.height / 2;
    const left = Math.max(
      HEATMAP_TOOLTIP_VIEWPORT_PADDING,
      Math.min(pointerX + HEATMAP_TOOLTIP_CURSOR_OFFSET, viewportWidth - HEATMAP_TOOLTIP_MAX_WIDTH - HEATMAP_TOOLTIP_VIEWPORT_PADDING),
    );
    const placement = pointerY > viewportHeight - 220 ? 'above' : 'below';
    const y = pointerY + (placement === 'above' ? -HEATMAP_TOOLTIP_CURSOR_OFFSET : HEATMAP_TOOLTIP_CURSOR_OFFSET);
    setTooltip({ lines, x: left, y, placement });
  };
  const hideTooltip = () => setTooltip(null);
  return (
    <section className={`${styles.analysisCard} ${styles.heatmapCard} ${isDark ? styles.heatmapCardDark : styles.heatmapCardLight}`}>
      <div className={styles.cardHeader}>
        <div>
          <h2>{t('usage_stats.analysis_heatmap_title')}</h2>
          <p>{t('usage_stats.analysis_heatmap_subtitle')}</p>
        </div>
      </div>
      {loading ? (
        <div className={styles.emptyState}>{t('common.loading')}</div>
      ) : cells.length === 0 ? (
        <div className={styles.emptyState}>{t('usage_stats.no_data')}</div>
      ) : (
        <>
          <div className={styles.analysisChartSurface}>
            <div className={styles.heatmapScroller}>
              <div className={styles.heatmapGrid} style={{ gridTemplateColumns: `150px repeat(${models.length}, minmax(82px, 1fr))` }}>
                <div className={styles.heatmapCorner}>{t('usage_stats.analysis_heatmap_api_key')}</div>
                {models.map((model) => (
                  <div
                    key={model}
                    className={`${styles.heatmapHeaderCell} ${styles.heatmapModelHeaderCell}`}
                    data-full-name={model}
                    title={model}
                    tabIndex={0}
                    aria-label={model}
                    onMouseEnter={(event) => showTooltip([model], event)}
                    onMouseMove={(event) => showTooltip([model], event)}
                    onMouseLeave={hideTooltip}
                    onFocus={(event) => showTooltip([model], event)}
                    onBlur={hideTooltip}
                  >
                    <span className={`${styles.heatmapTruncatedLabel} ${styles.heatmapModelLabel}`}>{model}</span>
                  </div>
                ))}
                {apiKeys.map((apiKey) => {
                  const apiKeyLabel = getAPIKeyLabel(apiKey);
                  return (
                    <div key={apiKey} className={styles.heatmapRowContents}>
                      <div className={`${styles.heatmapRowLabel} ${styles.heatmapTooltipTarget}`} data-full-name={apiKeyLabel}>
                        <span className={styles.heatmapTruncatedLabel}>{apiKeyLabel}</span>
                      </div>
                      {models.map((model) => {
                        const cell = cellMap.get(`${apiKey}\0${model}`);
                        const heatmapTokens = toNumber(cell?.total_tokens);
                        const intensity = getHeatmapVisualIntensity(heatmapTokens, maxHeatmapTokens);
                        const tooltipLines = buildTooltipLines(apiKey, model, cell);
                        return (
                          <div
                            key={`${apiKey}-${model}`}
                            className={styles.heatmapCell}
                            style={{
                              background: getHeatmapCellColor(intensity, isDark),
                              color: getHeatmapCellTextColor(intensity, isDark),
                              '--heatmap-flame-alpha': intensity.toFixed(3),
                            } as CSSProperties}
                            tabIndex={0}
                            aria-label={tooltipLines.join(', ')}
                            data-tooltip={tooltipLines.join('\n')}
                            onMouseEnter={(event) => showTooltip(tooltipLines, event)}
                            onMouseMove={(event) => showTooltip(tooltipLines, event)}
                            onMouseLeave={hideTooltip}
                            onFocus={(event) => showTooltip(tooltipLines, event)}
                            onBlur={hideTooltip}
                          >
                            <span className={styles.heatmapCellTokenValue}>
                              {formatCompactNumber(heatmapTokens)}
                            </span>
                          </div>
                        );
                      })}
                    </div>
                  );
                })}
              </div>
            </div>
          </div>
          <div className={styles.heatmapLegend} aria-label={t('usage_stats.analysis_heatmap_legend')}>
            <span>{t('usage_stats.analysis_heatmap_low')}</span>
            <span className={styles.heatmapLegendRamp} aria-hidden="true" />
            <span>{t('usage_stats.analysis_heatmap_high')}</span>
          </div>
          {tooltip ? (
            <div
              className={styles.heatmapFloatingTooltip}
              role="tooltip"
              style={{
                left: tooltip.x,
                top: tooltip.y,
                transform: tooltip.placement === 'above' ? 'translateY(-100%)' : undefined,
              }}
            >
              {tooltip.lines.map((line, index) => (
                <span key={`${index}-${line}`} className={index === 0 ? styles.heatmapTooltipTitle : ''}>{line}</span>
              ))}
            </div>
          ) : null}
        </>
      )}
    </section>
  );
}

export function AnalysisPanel({ analysis, loading, isDark, isMobile }: AnalysisPanelProps) {
  const { t } = useTranslation();
  const tokenRows = useMemo(() => buildTokenUsageRows(analysis?.token_usage ?? [], analysis?.granularity ?? 'hourly'), [analysis]);
  const apiComposition = useMemo(() => takeMajorComposition(analysis?.api_key_composition ?? [], t('usage_stats.analysis_others')), [analysis, t]);
  const modelComposition = useMemo(() => takeMajorComposition(analysis?.model_composition ?? [], t('usage_stats.analysis_others')), [analysis, t]);
  const authFilesComposition = useMemo(() => takeMajorComposition(analysis?.auth_files_composition ?? [], t('usage_stats.analysis_others')), [analysis, t]);
  const aiProviderComposition = useMemo(() => takeMajorComposition(analysis?.ai_provider_composition ?? [], t('usage_stats.analysis_others')), [analysis, t]);
  const compositionTabs = useMemo<CompositionTab[]>(() => [
    { id: 'api_key', label: t('usage_stats.analysis_composition_api_key_tab'), items: apiComposition },
    { id: 'model', label: t('usage_stats.analysis_composition_model_tab'), items: modelComposition },
    { id: 'auth_files', label: t('usage_stats.analysis_composition_auth_files_tab'), items: authFilesComposition },
    { id: 'ai_provider', label: t('usage_stats.analysis_composition_ai_provider_tab'), items: aiProviderComposition },
  ], [apiComposition, modelComposition, authFilesComposition, aiProviderComposition, t]);

  return (
    <div className={styles.analysisPanel}>
      <TokenUsageChart rows={tokenRows} loading={loading} isDark={isDark} isMobile={isMobile} />
      <div className={styles.insightGrid}>
        <CostBreakdownCard breakdown={analysis?.cost_breakdown} rows={tokenRows} loading={loading} />
        <ModelEfficiencyCard rows={analysis?.model_efficiency ?? []} loading={loading} isDark={isDark} isMobile={isMobile} />
      </div>
      <CompositionPanel tabs={compositionTabs} loading={loading} isDark={isDark} />
      <Heatmap cells={analysis?.heatmap?.cells ?? []} apiKeys={analysis?.heatmap?.api_keys ?? []} apiKeyLabels={analysis?.heatmap?.api_key_labels ?? {}} models={analysis?.heatmap?.models ?? []} loading={loading} isDark={isDark} />
    </div>
  );
}
