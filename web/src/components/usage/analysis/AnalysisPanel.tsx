import { memo, useCallback, useEffect, useMemo, useRef, useState, type ComponentProps, type CSSProperties, type FocusEvent, type MouseEvent, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Base, Line, Pie, type LineConfig } from '@ant-design/charts';
import type {
  AnalysisCompositionItem,
  AnalysisCostBreakdown,
  AnalysisHeatmapCell,
  AnalysisLatencyDiagnostics,
  AnalysisModelEfficiencyItem,
  AnalysisResponse,
  AnalysisTokenUsageBucket,
} from '@/lib/types';
import {
  calculateDisplayInputTokens,
  calculateDisplayOutputTokens,
  formatCompactNumber,
  formatDurationMs,
  formatPerMinuteValue,
  formatUsd,
} from '@/utils/usage';
import { SectionHeader } from '@/components/layout';
import { getChartTheme, type KeeperChartTheme } from '@/lib/chartTheme';
import { PricingCoverageNotice, normalizeUnpricedModels } from '../PricingCoverageNotice';
import styles from './AnalysisPanel.module.scss';

interface AnalysisPanelProps {
  analysis: AnalysisResponse | null;
  loading: boolean;
  isDark: boolean;
  isMobile: boolean;
  onConfigurePricing?: () => void;
}

type BaseConfig = ComponentProps<typeof Base>;
type PieConfig = ComponentProps<typeof Pie>;

type ChartRow = {
  label: string;
  input: number;
  output: number;
  rawInput: number;
  rawOutput: number;
  cacheRead: number;
  cacheWrite: number;
  reasoning: number;
  total: number;
  requests: number;
  cost: number;
};

type LegendItem = {
  label: string;
  color: string;
};

type TokenLabels = {
  time: string;
  input: string;
  output: string;
  cacheRead: string;
  cacheWrite: string;
  reasoning: string;
  total: string;
  average: string;
  requests: string;
  cost: string;
};

type TokenColors = {
  input: string;
  output: string;
  cacheRead: string;
  cacheWrite: string;
  reasoning: string;
  requests: string;
  cost: string;
};

type TokenStackDatum = {
  label: string;
  series: string;
  value: number;
  rawValue: number;
  total: number;
  color: string;
};

type TokenLineDatum = {
  label: string;
  value: number;
};

type FloatingTooltipState = {
  lines: string[];
  x: number;
  y: number;
  placement: 'above' | 'below';
};

type CostBreakdownSegmentKey = 'input' | 'cacheRead' | 'cacheWrite' | 'output';
type CostBreakdownSegment = {
  key: CostBreakdownSegmentKey;
  label: string;
  value: number;
  color: string;
  tokens: number;
};

type LatencyScatterPoint = {
  ttft: number;
  latency: number;
};

type LatencyThemeColors = {
  point: string;
  pointFill: string;
  p95TTFT: string;
  p95Latency: string;
};

type CompositionDatum = {
  key: string;
  label: string;
  totalTokens: number;
  color: string;
};

type EfficiencyPoint = {
  model: string;
  requests: number;
  cost: number;
  totalTokens: number;
  costPerMillion: number;
  cacheReadRate: number;
  radius: number;
  hoverRadius: number;
  color: string;
};

const COST_TOOLTIP_MAX_WIDTH = 280;
const COST_TOOLTIP_VIEWPORT_PADDING = 8;
const COST_TOOLTIP_CURSOR_OFFSET = 14;
const COMPOSITION_DONUT_BORDER_RADIUS = 10;
const COMPOSITION_DONUT_SPACING = 4;
const COMPOSITION_DONUT_HOVER_OFFSET = 10;
const COMPOSITION_DONUT_LAYOUT_PADDING = 28;
const COMPOSITION_TOOLTIP_TITLE_LINE_LENGTH = 28;
const COMPOSITION_TOOLTIP_TITLE_MAX_LINES = 3;
const HEATMAP_TOOLTIP_MAX_WIDTH = 280;
const HEATMAP_TOOLTIP_VIEWPORT_PADDING = 8;
const HEATMAP_TOOLTIP_CURSOR_OFFSET = 14;
const MODEL_EFFICIENCY_MIN_RADIUS = 5;
const MODEL_EFFICIENCY_MAX_RADIUS = 24;
const MODEL_EFFICIENCY_HOVER_RADIUS_DELTA = 4;
const MODEL_EFFICIENCY_RADIUS_EASING = 0.75;
const MODEL_EFFICIENCY_OUTLIER_RATIO = 8;
const MODEL_EFFICIENCY_AXIS_PADDING_FACTOR = 2.5;
const EMPTY_COMPOSITION_ITEMS: AnalysisCompositionItem[] = [];
const CATEGORICAL_EXTRA_COLORS = {
  light: ['#4a3aa7', '#e34948'],
  dark: ['#9085e9', '#e66767'],
} as const;

const toNumber = (value: unknown) => {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : 0;
};

const formatPercent = (value: number) => `${value.toFixed(2)}%`;
const clampPercent = (value: number) => Math.max(0, Math.min(100, value));

// Palettes only depend on the theme flag; cache per mode so callers in tight
// loops (getStableEntityColor, chart config builders) reuse one array.
const ENTITY_PALETTE_CACHE = new Map<boolean, string[]>();
const getEntityPalette = (isDark: boolean): string[] => {
  const cached = ENTITY_PALETTE_CACHE.get(isDark);
  if (cached) return cached;
  const series = getChartTheme(isDark).series;
  const palette = [
    series.blue.stroke,
    series.cyan.stroke,
    series.indigo.stroke,
    series.violet.stroke,
    series.teal.stroke,
    series.orange.stroke,
    ...(isDark ? CATEGORICAL_EXTRA_COLORS.dark : CATEGORICAL_EXTRA_COLORS.light),
  ];
  ENTITY_PALETTE_CACHE.set(isDark, palette);
  return palette;
};

const hashEntityKey = (key: string): number => {
  let hash = 2166136261;
  for (let index = 0; index < key.length; index += 1) {
    hash ^= key.charCodeAt(index);
    hash = Math.imul(hash, 16777619);
  }
  return hash >>> 0;
};

const getStableEntityColor = (key: string, isDark: boolean): string => {
  const palette = getEntityPalette(isDark);
  return palette[hashEntityKey(key) % palette.length];
};

const TOKEN_COLORS_CACHE = new Map<boolean, TokenColors>();
const getTokenColors = (isDark: boolean): TokenColors => {
  const cached = TOKEN_COLORS_CACHE.get(isDark);
  if (cached) return cached;
  const series = getChartTheme(isDark).series;
  const colors: TokenColors = {
    input: series.blue.stroke,
    output: series.cyan.stroke,
    cacheRead: series.teal.stroke,
    cacheWrite: series.orange.stroke,
    reasoning: series.violet.stroke,
    requests: series.indigo.stroke,
    cost: series.orange.stroke,
  };
  TOKEN_COLORS_CACHE.set(isDark, colors);
  return colors;
};

const getLatencyColors = (isDark: boolean): LatencyThemeColors => {
  const series = getChartTheme(isDark).series;
  return {
    point: series.indigo.stroke,
    pointFill: series.indigo.fill,
    p95TTFT: series.cyan.stroke,
    p95Latency: series.violet.stroke,
  };
};

const interpolateColor = (from: [number, number, number], to: [number, number, number], ratio: number) => {
  const clampedRatio = Math.max(0, Math.min(1, ratio));
  return from.map((channel, index) => Math.round(channel + (to[index] - channel) * clampedRatio));
};

const getHeatmapCellColor = (intensity: number, isDark: boolean) => {
  const clampedIntensity = Math.max(0, Math.min(1, intensity));
  const stops: Array<{ at: number; color: [number, number, number] }> = [
    ...(isDark
      ? [
        { at: 0, color: [58, 36, 48] },
        { at: 0.46, color: [122, 47, 59] },
        { at: 1, color: [239, 68, 68] },
      ] satisfies Array<{ at: number; color: [number, number, number] }>
      : [
        { at: 0, color: [255, 247, 237] },
        { at: 0.34, color: [254, 215, 170] },
        { at: 0.67, color: [251, 146, 60] },
        { at: 1, color: [239, 68, 68] },
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

const getIntlTimeZone = (timezone: string | undefined) => {
  const trimmed = timezone?.trim();
  if (!trimmed || trimmed === 'Local') return undefined;
  return trimmed;
};

const formatBucketLabelFromLiteral = (bucket: string, granularity: AnalysisResponse['granularity']) => {
  const match = bucket.match(/^(\d{4})-(\d{2})-(\d{2})(?:[T\s](\d{2}))?/);
  if (!match) return null;
  const month = Number(match[2]);
  const day = Number(match[3]);
  const hour = match[4] ? Number(match[4]) : NaN;
  if (month < 1 || month > 12 || day < 1 || day > 31) return null;
  if (granularity === 'daily') {
    return `${month}/${day}`;
  }
  if (!Number.isFinite(hour) || hour < 0 || hour > 23) return null;
  return `${String(hour).padStart(2, '0')}:00`;
};

const formatBucketLabel = (bucket: string, granularity: AnalysisResponse['granularity'], timezone?: string) => {
  const date = new Date(bucket);
  if (Number.isNaN(date.getTime())) return bucket;
  const timeZone = getIntlTimeZone(timezone);
  try {
    if (granularity === 'daily') {
      return new Intl.DateTimeFormat('en-US', { month: 'numeric', day: 'numeric', timeZone }).format(date);
    }
    const hour = new Intl.DateTimeFormat('en-GB', { hour: '2-digit', hourCycle: 'h23', timeZone }).format(date);
    return `${hour}:00`;
  } catch {
    const literalLabel = formatBucketLabelFromLiteral(bucket, granularity);
    if (literalLabel) return literalLabel;
  }
  if (granularity === 'daily') {
    return `${date.getMonth() + 1}/${date.getDate()}`;
  }
  return `${String(date.getHours()).padStart(2, '0')}:00`;
};

function buildTokenUsageRows(buckets: AnalysisTokenUsageBucket[], granularity: AnalysisResponse['granularity'], timezone?: string): ChartRow[] {
  return buckets.map((bucket) => ({
    label: formatBucketLabel(bucket.bucket, granularity, timezone),
    input: calculateDisplayInputTokens({
      inputTokens: bucket.input_tokens,
      cacheReadTokens: bucket.cache_read_tokens,
      cacheCreationTokens: bucket.cache_creation_tokens,
    }),
    output: calculateDisplayOutputTokens({
      outputTokens: bucket.output_tokens,
      reasoningTokens: bucket.reasoning_tokens,
    }),
    rawInput: toNumber(bucket.input_tokens),
    rawOutput: toNumber(bucket.output_tokens),
    cacheRead: toNumber(bucket.cache_read_tokens),
    cacheWrite: toNumber(bucket.cache_creation_tokens),
    reasoning: toNumber(bucket.reasoning_tokens),
    total: toNumber(bucket.total_tokens),
    requests: toNumber(bucket.requests),
    cost: toNumber(bucket.cost_usd),
  }));
}

function calculateAverageTotalTokens(rows: ChartRow[]): number {
  if (rows.length === 0) return 0;
  return rows.reduce((sum, row) => sum + row.total, 0) / rows.length;
}

function calculateAnalysisWindowMinutes(analysis: AnalysisResponse | null): number | null {
  const start = Date.parse(analysis?.range_start ?? '');
  const end = Date.parse(analysis?.range_end ?? '');
  if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start) return null;
  return (end - start) / 60_000;
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
      cache_read_tokens: sum.cache_read_tokens + toNumber(item.cache_read_tokens),
      cache_creation_tokens: sum.cache_creation_tokens + toNumber(item.cache_creation_tokens),
      reasoning_tokens: sum.reasoning_tokens + toNumber(item.reasoning_tokens),
      cost_usd: sum.cost_usd + toNumber(item.cost_usd),
      cost_available: sum.cost_available && item.cost_available !== false,
    }),
    { total_tokens: 0, requests: 0, input_tokens: 0, output_tokens: 0, cache_read_tokens: 0, cache_creation_tokens: 0, reasoning_tokens: 0, cost_usd: 0, cost_available: true },
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
      cache_read_tokens: rest.cache_read_tokens,
      cache_creation_tokens: rest.cache_creation_tokens,
      reasoning_tokens: rest.reasoning_tokens,
      cost_usd: rest.cost_usd,
      cost_available: rest.cost_available,
      percent: total > 0 ? (rest.total_tokens / total) * 100 : 0,
    },
  ];
}

function AnalysisCardHeader({ title, subtitle }: { title: string; subtitle: string }) {
  return (
    <SectionHeader
      className={styles.cardHeader}
      headingLevel={2}
      title={title}
      description={subtitle}
    />
  );
}

function ChartLegend({ label, items }: { label: string; items: LegendItem[] }) {
  return (
    <div className={styles.analysisChartLegend} aria-label={label}>
      {items.map((item) => (
        <div key={`${item.label}-${item.color}`} className={styles.analysisLegendItem} title={item.label}>
          <span className={styles.analysisLegendDot} style={{ backgroundColor: item.color }} />
          <span className={styles.analysisLegendLabel}>{item.label}</span>
        </div>
      ))}
    </div>
  );
}

const buildTokenLegendItems = (labels: TokenLabels, averageTokenTotal: number, colors: TokenColors, averageLineColor: string): LegendItem[] => [
  { label: labels.input, color: colors.input },
  { label: labels.output, color: colors.output },
  { label: labels.cacheRead, color: colors.cacheRead },
  { label: labels.cacheWrite, color: colors.cacheWrite },
  { label: labels.reasoning, color: colors.reasoning },
  { label: labels.requests, color: colors.requests },
  { label: labels.cost, color: colors.cost },
  { label: `${labels.average}: ${formatCompactNumber(averageTokenTotal)}`, color: averageLineColor },
];

function buildTokenStackData(rows: ChartRow[], labels: TokenLabels, colors: TokenColors): TokenStackDatum[] {
  return rows.flatMap((row) => [
    { label: row.label, series: labels.input, value: row.input, rawValue: row.rawInput, total: row.total, color: colors.input },
    { label: row.label, series: labels.output, value: row.output, rawValue: row.rawOutput, total: row.total, color: colors.output },
    { label: row.label, series: labels.cacheRead, value: row.cacheRead, rawValue: row.cacheRead, total: row.total, color: colors.cacheRead },
    { label: row.label, series: labels.cacheWrite, value: row.cacheWrite, rawValue: row.cacheWrite, total: row.total, color: colors.cacheWrite },
    { label: row.label, series: labels.reasoning, value: row.reasoning, rawValue: row.reasoning, total: row.total, color: colors.reasoning },
  ]);
}

function buildTokenStackConfig({
  data,
  labels,
  colors,
  averageTokenTotal,
  chartTheme,
  isDark,
  isMobile,
}: {
  data: TokenStackDatum[];
  labels: TokenLabels;
  colors: TokenColors;
  averageTokenTotal: number;
  chartTheme: KeeperChartTheme;
  isDark: boolean;
  isMobile: boolean;
}): BaseConfig {
  return {
    autoFit: true,
    animate: false,
    legend: false,
    theme: { type: isDark ? 'classicDark' : 'classic' },
    scale: {
      y: { type: 'linear', domainMin: 0, nice: true },
      color: {
        domain: [labels.input, labels.output, labels.cacheRead, labels.cacheWrite, labels.reasoning],
        range: [colors.input, colors.output, colors.cacheRead, colors.cacheWrite, colors.reasoning],
      },
    },
    axis: {
      x: {
        title: false,
        grid: false,
        tickCount: isMobile ? 5 : 10,
        labelFill: chartTheme.textSecondary,
        labelFontSize: isMobile ? 10 : 11,
        line: true,
        lineStroke: chartTheme.axis,
        tickStroke: chartTheme.axis,
      },
      y: {
        title: false,
        grid: true,
        tickCount: 5,
        labelFormatter: (value: number) => formatCompactNumber(Number(value)),
        labelFill: chartTheme.textSecondary,
        labelFontSize: isMobile ? 10 : 11,
        gridStroke: chartTheme.grid,
        line: true,
        lineStroke: chartTheme.axis,
        tickStroke: chartTheme.axis,
      },
    },
    interaction: {
      tooltip: {
        shared: true,
        crosshairs: true,
        marker: false,
      },
    },
    children: [
      {
        type: 'interval',
        data,
        encode: { x: 'label', y: 'value', color: 'series' },
        transform: [{ type: 'stackY' }],
        style: {
          maxWidth: 24,
          inset: 1,
          radiusTopLeft: 4,
          radiusTopRight: 4,
        },
        state: {
          active: { fillOpacity: 0.82 },
        },
        tooltip: {
          title: (datum: TokenStackDatum) => datum.label,
          items: [(datum: TokenStackDatum) => ({
            name: datum.series,
            color: datum.color,
            value: `${formatCompactNumber(datum.rawValue)} · ${labels.total}: ${formatCompactNumber(datum.total)}`,
          })],
        },
      },
      {
        type: 'lineY',
        data: [{ value: averageTokenTotal }],
        encode: { y: 'value' },
        style: {
          stroke: chartTheme.averageLine,
          lineWidth: 1.5,
          lineDash: [5, 5],
        },
        tooltip: false,
      },
    ],
  };
}

function buildTokenLineConfig({
  rows,
  field,
  label,
  color,
  chartTheme,
  isDark,
  isMobile,
  valueFormatter,
}: {
  rows: ChartRow[];
  field: 'requests' | 'cost';
  label: string;
  color: string;
  chartTheme: KeeperChartTheme;
  isDark: boolean;
  isMobile: boolean;
  valueFormatter: (value: number) => string;
}): LineConfig {
  const data: TokenLineDatum[] = rows.map((row) => ({ label: row.label, value: row[field] }));
  return {
    data,
    xField: 'label',
    yField: 'value',
    shapeField: 'smooth',
    autoFit: true,
    animate: false,
    legend: false,
    theme: { type: isDark ? 'classicDark' : 'classic' },
    scale: {
      y: { type: 'linear', domainMin: 0, nice: true },
    },
    axis: {
      x: {
        title: false,
        grid: false,
        tickCount: isMobile ? 4 : 8,
        labelFill: chartTheme.textSecondary,
        labelFontSize: 10,
        line: true,
        lineStroke: chartTheme.axis,
        tickStroke: chartTheme.axis,
      },
      y: {
        title: false,
        grid: true,
        tickCount: 4,
        labelFormatter: (value: number) => valueFormatter(Number(value)),
        labelFill: chartTheme.textSecondary,
        labelFontSize: 10,
        gridStroke: chartTheme.grid,
        line: true,
        lineStroke: chartTheme.axis,
        tickStroke: chartTheme.axis,
      },
    },
    style: {
      stroke: color,
      lineWidth: 2,
      lineDash: field === 'requests' ? [6, 4] : undefined,
    },
    point: field === 'cost' ? {
      style: {
        fill: color,
        stroke: chartTheme.tooltipBackground,
        lineWidth: 2,
        r: 4,
      },
      state: { active: { r: 6 } },
      tooltip: false,
    } : undefined,
    tooltip: {
      title: 'label',
      items: [{
        name: label,
        color,
        field: 'value',
        valueFormatter: (value: number) => valueFormatter(Number(value)),
      }],
    },
    interaction: {
      tooltip: {
        shared: true,
        crosshairs: true,
        marker: true,
      },
    },
  };
}

function TokenUsageTable({ rows, labels }: { rows: ChartRow[]; labels: TokenLabels }) {
  return (
    <table className={styles.chartDataTable}>
      <caption>{labels.total}</caption>
      <thead>
        <tr>
          <th scope="col">{labels.time}</th>
          <th scope="col">{labels.input}</th>
          <th scope="col">{labels.output}</th>
          <th scope="col">{labels.cacheRead}</th>
          <th scope="col">{labels.cacheWrite}</th>
          <th scope="col">{labels.reasoning}</th>
          <th scope="col">{labels.total}</th>
          <th scope="col">{labels.requests}</th>
          <th scope="col">{labels.cost}</th>
        </tr>
      </thead>
      <tbody>
        {rows.map((row) => (
          <tr key={row.label}>
            <th scope="row">{row.label}</th>
            <td>{formatCompactNumber(row.rawInput)}</td>
            <td>{formatCompactNumber(row.rawOutput)}</td>
            <td>{formatCompactNumber(row.cacheRead)}</td>
            <td>{formatCompactNumber(row.cacheWrite)}</td>
            <td>{formatCompactNumber(row.reasoning)}</td>
            <td>{formatCompactNumber(row.total)}</td>
            <td>{formatCompactNumber(row.requests)}</td>
            <td>{formatUsd(row.cost)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function TokenUsageChart({ rows, loading, isDark, isMobile }: { rows: ChartRow[]; loading: boolean; isDark: boolean; isMobile: boolean }) {
  const { t } = useTranslation();
  const tokenLabels = useMemo(() => ({
    time: t('usage_stats.time'),
    input: t('usage_stats.input_tokens'),
    output: t('usage_stats.output_tokens'),
    cacheRead: t('usage_stats.cache_read_tokens'),
    cacheWrite: t('usage_stats.cache_creation_tokens'),
    reasoning: t('usage_stats.reasoning_tokens'),
    total: t('usage_stats.total_tokens'),
    average: t('usage_stats.analysis_token_average'),
    requests: t('usage_stats.requests_count'),
    cost: t('usage_stats.total_cost'),
  }), [t]);
  const chartTheme = useMemo(() => getChartTheme(isDark), [isDark]);
  const colors = useMemo(() => getTokenColors(isDark), [isDark]);
  const averageTokenTotal = useMemo(() => calculateAverageTotalTokens(rows), [rows]);
  const tokenData = useMemo(() => buildTokenStackData(rows, tokenLabels, colors), [colors, rows, tokenLabels]);
  const tokenConfig = useMemo(() => buildTokenStackConfig({
    data: tokenData,
    labels: tokenLabels,
    colors,
    averageTokenTotal,
    chartTheme,
    isDark,
    isMobile,
  }), [averageTokenTotal, chartTheme, colors, isDark, isMobile, tokenData, tokenLabels]);
  const requestConfig = useMemo(() => buildTokenLineConfig({
    rows,
    field: 'requests',
    label: tokenLabels.requests,
    color: colors.requests,
    chartTheme,
    isDark,
    isMobile,
    valueFormatter: formatCompactNumber,
  }), [chartTheme, colors.requests, isDark, isMobile, rows, tokenLabels.requests]);
  const costConfig = useMemo(() => buildTokenLineConfig({
    rows,
    field: 'cost',
    label: tokenLabels.cost,
    color: colors.cost,
    chartTheme,
    isDark,
    isMobile,
    valueFormatter: formatUsd,
  }), [chartTheme, colors.cost, isDark, isMobile, rows, tokenLabels.cost]);
  const legendItems = useMemo(
    () => buildTokenLegendItems(tokenLabels, averageTokenTotal, colors, chartTheme.averageLine),
    [averageTokenTotal, chartTheme.averageLine, colors, tokenLabels],
  );
  return (
    <section className={`${styles.analysisCard} ${styles.tokenUsageCard}`}>
      <AnalysisCardHeader
        title={t('usage_stats.analysis_token_usage_title')}
        subtitle={t('usage_stats.analysis_token_usage_subtitle')}
      />
      {loading ? (
        <div className={styles.emptyState}>{t('common.loading')}</div>
      ) : rows.length === 0 ? (
        <div className={styles.emptyState}>{t('usage_stats.no_data')}</div>
      ) : (
        <div className={styles.analysisChartSurface}>
          <ChartLegend label={t('usage_stats.analysis_token_usage_title')} items={legendItems} />
          <div className={styles.tokenChartFrame} aria-label={tokenLabels.total}>
            <Base {...tokenConfig} />
          </div>
          <div className={styles.tokenSmallMultiples}>
            <div className={styles.tokenMetricChart}>
              <h3>{tokenLabels.requests}</h3>
              <div className={styles.tokenMetricChartFrame} aria-label={tokenLabels.requests}>
                <Line {...requestConfig} />
              </div>
            </div>
            <div className={styles.tokenMetricChart}>
              <h3>{tokenLabels.cost}</h3>
              <div className={styles.tokenMetricChartFrame} aria-label={tokenLabels.cost}>
                <Line {...costConfig} />
              </div>
            </div>
          </div>
          <TokenUsageTable rows={rows} labels={tokenLabels} />
        </div>
      )}
    </section>
  );
}

const emptyLatencyDiagnostics = (): AnalysisLatencyDiagnostics => ({
  points: [],
  density: [],
  total_points: 0,
  sampled: false,
  p95_ttft_ms: 0,
  p95_latency_ms: 0,
  max_ttft_ms: 0,
  max_latency_ms: 0,
});

const getLatencyLogAxisBounds = (values: Iterable<number>) => {
  let minValue = Number.POSITIVE_INFINITY;
  let maxValue = 0;
  for (const value of values) {
    if (!Number.isFinite(value) || value <= 0) continue;
    if (value < minValue) minValue = value;
    if (value > maxValue) maxValue = value;
  }
  if (!Number.isFinite(minValue) || maxValue <= 0) {
    return { min: 1, max: 10 };
  }
  return {
    min: Math.max(1, Math.floor(minValue / 1.35)),
    max: Math.max(10, Math.ceil(maxValue * 1.18)),
  };
};

function* getLatencyAxisValues(diagnostics: AnalysisLatencyDiagnostics, axis: 'ttft' | 'latency'): Generator<number> {
  yield axis === 'ttft' ? diagnostics.max_ttft_ms : diagnostics.max_latency_ms;
  yield axis === 'ttft' ? diagnostics.p95_ttft_ms : diagnostics.p95_latency_ms;
  for (const point of diagnostics.points) {
    yield axis === 'ttft' ? point.ttft_ms : point.latency_ms;
  }
}

function buildLatencyDiagnosticsConfig({
  diagnostics,
  labels,
  colors,
  chartTheme,
  isDark,
  isMobile,
}: {
  diagnostics: AnalysisLatencyDiagnostics;
  labels: { ttft: string; latency: string; samples: string; p95TTFT: string; p95Latency: string };
  colors: LatencyThemeColors;
  chartTheme: KeeperChartTheme;
  isDark: boolean;
  isMobile: boolean;
}): BaseConfig {
  const data: LatencyScatterPoint[] = diagnostics.points
    .map((point) => ({ ttft: toNumber(point.ttft_ms), latency: toNumber(point.latency_ms) }))
    .filter((point) => point.ttft > 0 && point.latency > 0);
  const xBounds = getLatencyLogAxisBounds(getLatencyAxisValues(diagnostics, 'ttft'));
  const yBounds = getLatencyLogAxisBounds(getLatencyAxisValues(diagnostics, 'latency'));
  const children: Array<Record<string, unknown>> = [
    {
      type: 'point',
      data,
      encode: { x: 'ttft', y: 'latency' },
      style: {
        fill: colors.pointFill,
        stroke: 'transparent',
        lineWidth: 0,
        r: 3,
      },
      state: {
        active: { r: 5, fill: colors.point },
      },
      tooltip: {
        title: () => labels.samples,
        items: [
          (datum: LatencyScatterPoint) => ({ name: labels.ttft, color: colors.point, value: formatDurationMs(datum.ttft) }),
          (datum: LatencyScatterPoint) => ({ name: labels.latency, color: colors.point, value: formatDurationMs(datum.latency) }),
        ],
      },
    },
  ];
  if (toNumber(diagnostics.p95_ttft_ms) > 0) {
    children.push({
      type: 'lineX',
      data: [{ value: toNumber(diagnostics.p95_ttft_ms) }],
      encode: { x: 'value' },
      style: { stroke: colors.p95TTFT, lineWidth: 1.5, lineDash: [5, 5] },
      tooltip: {
        title: () => labels.p95TTFT,
        items: [() => ({ name: labels.p95TTFT, color: colors.p95TTFT, value: formatDurationMs(diagnostics.p95_ttft_ms) })],
      },
    });
  }
  if (toNumber(diagnostics.p95_latency_ms) > 0) {
    children.push({
      type: 'lineY',
      data: [{ value: toNumber(diagnostics.p95_latency_ms) }],
      encode: { y: 'value' },
      style: { stroke: colors.p95Latency, lineWidth: 1.5, lineDash: [5, 5] },
      tooltip: {
        title: () => labels.p95Latency,
        items: [() => ({ name: labels.p95Latency, color: colors.p95Latency, value: formatDurationMs(diagnostics.p95_latency_ms) })],
      },
    });
  }

  return {
    autoFit: true,
    animate: false,
    legend: false,
    theme: { type: isDark ? 'classicDark' : 'classic' },
    scale: {
      x: { type: 'log', domainMin: xBounds.min, domainMax: xBounds.max, nice: false },
      y: { type: 'log', domainMin: yBounds.min, domainMax: yBounds.max, nice: false },
    },
    axis: {
      x: {
        title: labels.ttft,
        grid: true,
        tickCount: isMobile ? 4 : 6,
        labelFormatter: (value: number) => formatDurationMs(Number(value)),
        labelFill: chartTheme.textSecondary,
        labelFontSize: 10,
        titleFill: chartTheme.textSecondary,
        gridStroke: chartTheme.grid,
        line: true,
        lineStroke: chartTheme.axis,
        tickStroke: chartTheme.axis,
      },
      y: {
        title: labels.latency,
        grid: true,
        tickCount: isMobile ? 4 : 6,
        labelFormatter: (value: number) => formatDurationMs(Number(value)),
        labelFill: chartTheme.textSecondary,
        labelFontSize: 10,
        titleFill: chartTheme.textSecondary,
        gridStroke: chartTheme.grid,
        line: true,
        lineStroke: chartTheme.axis,
        tickStroke: chartTheme.axis,
      },
    },
    interaction: {
      tooltip: {
        shared: false,
        crosshairs: true,
        marker: true,
      },
    },
    children,
  };
}

function LatencyDiagnosticsCard({ diagnostics, loading, isDark, isMobile }: { diagnostics: AnalysisLatencyDiagnostics | undefined; loading: boolean; isDark: boolean; isMobile: boolean }) {
  const { t } = useTranslation();
  const safeDiagnostics = diagnostics ?? emptyLatencyDiagnostics();
  const chartTheme = useMemo(() => getChartTheme(isDark), [isDark]);
  const latencyColors = useMemo(() => getLatencyColors(isDark), [isDark]);
  const labels = useMemo(() => ({
    ttft: t('usage_stats.ttft'),
    latency: t('usage_stats.latency'),
    p95TTFT: t('usage_stats.analysis_latency_p95_ttft'),
    p95Latency: t('usage_stats.analysis_latency_p95_latency'),
    samples: t('usage_stats.analysis_latency_samples'),
  }), [t]);
  const chartConfig = useMemo(() => buildLatencyDiagnosticsConfig({
    diagnostics: safeDiagnostics,
    labels,
    colors: latencyColors,
    chartTheme,
    isDark,
    isMobile,
  }), [chartTheme, isDark, isMobile, labels, latencyColors, safeDiagnostics]);
  const legendItems = useMemo<LegendItem[]>(() => [
    { label: labels.samples, color: latencyColors.point },
    { label: labels.p95TTFT, color: latencyColors.p95TTFT },
    { label: labels.p95Latency, color: latencyColors.p95Latency },
  ], [labels, latencyColors]);
  const hasData = toNumber(safeDiagnostics.total_points) > 0 && safeDiagnostics.points.some((point) => toNumber(point.ttft_ms) > 0 && toNumber(point.latency_ms) > 0);

  return (
    <section className={`${styles.analysisCard} ${styles.latencyDiagnosticsCard}`}>
      <AnalysisCardHeader
        title={t('usage_stats.analysis_latency_title')}
        subtitle={t('usage_stats.analysis_latency_subtitle')}
      />
      {loading ? (
        <div className={styles.emptyState}>{t('common.loading')}</div>
      ) : !hasData ? (
        <div className={styles.emptyState}>{t('usage_stats.no_data')}</div>
      ) : (
        <div className={styles.latencyDiagnosticsBody}>
          <div className={styles.latencyMetricGrid}>
            <div className={styles.latencyMetric}>
              <span>{labels.p95TTFT}</span>
              <strong>{formatDurationMs(safeDiagnostics.p95_ttft_ms)}</strong>
            </div>
            <div className={styles.latencyMetric}>
              <span>{labels.p95Latency}</span>
              <strong>{formatDurationMs(safeDiagnostics.p95_latency_ms)}</strong>
            </div>
            <div className={styles.latencyMetric}>
              <span>{t('usage_stats.analysis_latency_samples_count')}</span>
              <strong>{formatCompactNumber(safeDiagnostics.total_points)}</strong>
              {safeDiagnostics.sampled ? <small>{t('usage_stats.analysis_latency_sampled')}</small> : null}
            </div>
          </div>
          <div className={styles.analysisChartSurface}>
            <ChartLegend label={t('usage_stats.analysis_latency_title')} items={legendItems} />
            <div className={styles.latencyChartFrame}>
              <Base {...chartConfig} />
            </div>
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

function CompositionMetaPill({ label, value }: { label: string; value: string }) {
  return (
    <span className={styles.compositionUsageMetaPill}>
      <span className={styles.compositionUsageMetaLabel}>{label}</span>
      <span className={styles.compositionUsageMetaValue}>{value}</span>
    </span>
  );
}

const formatCompositionRate = (value: number, windowMinutes: number | null): string => {
  if (!windowMinutes || windowMinutes <= 0) return '--';
  return formatPerMinuteValue(value / windowMinutes);
};

const toTruncatedTooltipTitleLine = (line: string): string => {
  const suffixLength = 3;
  const maxContentLength = COMPOSITION_TOOLTIP_TITLE_LINE_LENGTH - suffixLength;
  return `${line.slice(0, maxContentLength)}...`;
};

const wrapCompositionTooltipTitle = (label: unknown): string[] => {
  const normalized = String(label ?? '').replace(/\s+/g, ' ').trim();
  if (!normalized) return [];
  const lines: string[] = [];
  let remaining = normalized;

  while (remaining.length > 0) {
    if (remaining.length <= COMPOSITION_TOOLTIP_TITLE_LINE_LENGTH) {
      lines.push(remaining);
      break;
    }
    const naturalBreak = remaining.lastIndexOf(' ', COMPOSITION_TOOLTIP_TITLE_LINE_LENGTH);
    const breakIndex = naturalBreak > 0 ? naturalBreak : COMPOSITION_TOOLTIP_TITLE_LINE_LENGTH;
    lines.push(remaining.slice(0, breakIndex).trim());
    remaining = remaining.slice(breakIndex).trimStart();
  }

  if (lines.length <= COMPOSITION_TOOLTIP_TITLE_MAX_LINES) return lines;
  const visibleLines = lines.slice(0, COMPOSITION_TOOLTIP_TITLE_MAX_LINES);
  visibleLines[COMPOSITION_TOOLTIP_TITLE_MAX_LINES - 1] = toTruncatedTooltipTitleLine(visibleLines[COMPOSITION_TOOLTIP_TITLE_MAX_LINES - 1]);
  return visibleLines;
};

function buildCompositionConfig(items: AnalysisCompositionItem[], totalTokensLabel: string, isDark: boolean): PieConfig {
  const chartTheme = getChartTheme(isDark);
  const data: CompositionDatum[] = items.map((item) => ({
    key: item.key,
    label: item.label,
    totalTokens: toNumber(item.total_tokens),
    color: getStableEntityColor(item.key, isDark),
  }));
  return {
    data,
    angleField: 'totalTokens',
    colorField: 'key',
    radius: 0.88,
    innerRadius: 0.58,
    padding: COMPOSITION_DONUT_LAYOUT_PADDING,
    autoFit: true,
    animate: false,
    legend: false,
    theme: { type: isDark ? 'classicDark' : 'classic' },
    scale: {
      color: {
        domain: data.map((item) => item.key),
        range: data.map((item) => item.color),
      },
    },
    style: {
      radius: COMPOSITION_DONUT_BORDER_RADIUS,
      inset: COMPOSITION_DONUT_SPACING / 2,
      stroke: chartTheme.tooltipBackground,
      lineWidth: 2,
    },
    state: {
      active: {
        inset: -COMPOSITION_DONUT_HOVER_OFFSET / 5,
        lineWidth: 3,
      },
    },
    tooltip: {
      title: (datum: CompositionDatum) => wrapCompositionTooltipTitle(datum.label).join('\n'),
      items: [(datum: CompositionDatum) => ({
        name: totalTokensLabel,
        color: datum.color,
        value: formatCompactNumber(datum.totalTokens),
      })],
    },
    interaction: {
      elementHighlight: true,
      tooltip: { shared: false, crosshairs: false, marker: true },
    },
  };
}

function CompositionPanel({ tabs, loading, isDark, windowMinutes }: { tabs: CompositionTab[]; loading: boolean; isDark: boolean; windowMinutes: number | null }) {
  const { t } = useTranslation();
  const [activeTabId, setActiveTabId] = useState<CompositionTab['id']>('api_key');
  const activeTab = tabs.find((tab) => tab.id === activeTabId) ?? tabs[0];
  const items = activeTab?.items ?? EMPTY_COMPOSITION_ITEMS;
  // Key only by the active tab, not by the item set. A data refresh within the
  // same tab then updates the G2 canvas in place instead of remounting it; only
  // an actual tab switch (a different dataset) remounts.
  const activeContentKey = activeTab?.id ?? 'empty';
  const chartConfig = useMemo(() => buildCompositionConfig(items, t('usage_stats.total_tokens'), isDark), [isDark, items, t]);
  return (
    <section className={`${styles.analysisCard} ${styles.compositionCard}`}>
      <AnalysisCardHeader
        title={t('usage_stats.analysis_composition_title')}
        subtitle={t('usage_stats.analysis_composition_subtitle')}
      />
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
              <div className={styles.donutCanvasBox}>
                <Pie key={`chart-${activeContentKey}`} {...chartConfig} />
              </div>
            </div>
            <div key={`list-${activeContentKey}`} className={styles.compositionUsageList} aria-label={t('usage_stats.analysis_composition_title')}>
              {items.map((item) => {
                const rawPercent = toNumber(item.percent);
                const visualPercent = clampPercent(rawPercent);
                const color = getStableEntityColor(item.key, isDark);
                const barStyle = {
                  width: `${visualPercent}%`,
                  '--composition-bar-color': color,
                } as CSSProperties;
                return (
                  <div key={`${activeTab.id}-${item.key}`} className={styles.compositionUsageItem}>
                    <div className={styles.compositionUsageTopline}>
                      <span className={styles.compositionUsageLabel} title={item.label}>{item.label}</span>
                      <span className={styles.compositionUsageShare} aria-label={t('usage_stats.analysis_composition_token_percent')}>{formatPercent(rawPercent)}</span>
                    </div>
                    <div className={styles.compositionUsageTrack}>
                      {visualPercent > 0 && <span className={styles.compositionUsageBar} style={barStyle} />}
                    </div>
                    <div className={styles.compositionUsageMeta}>
                      <CompositionMetaPill label={t('usage_stats.total_tokens')} value={formatCompactNumber(toNumber(item.total_tokens))} />
                      <CompositionMetaPill label={t('usage_stats.requests_count')} value={formatCompactNumber(toNumber(item.requests))} />
                      <CompositionMetaPill label={t('usage_stats.total_cost')} value={formatUsd(toNumber(item.cost_usd))} />
                      <CompositionMetaPill label={t('usage_stats.rpm')} value={formatCompositionRate(toNumber(item.requests), windowMinutes)} />
                      <CompositionMetaPill label={t('usage_stats.tpm')} value={formatCompositionRate(toNumber(item.total_tokens), windowMinutes)} />
                    </div>
                  </div>
                );
              })}
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
      input: totals.input + Math.max(row.rawInput - row.cacheRead - row.cacheWrite, 0),
      cacheRead: totals.cacheRead + row.cacheRead,
      cacheWrite: totals.cacheWrite + row.cacheWrite,
      output: totals.output + row.rawOutput,
    }),
    { input: 0, cacheRead: 0, cacheWrite: 0, output: 0 },
  );
}

function CostBreakdownCard({ breakdown, rows, loading, isDark }: { breakdown: AnalysisCostBreakdown | undefined; rows: ChartRow[]; loading: boolean; isDark: boolean }) {
  const { t } = useTranslation();
  const [costTooltip, setCostTooltip] = useState<FloatingTooltipState | null>(null);
  // All of the cost derivations depend only on [breakdown, rows, isDark, t]; the
  // hover tooltip lives in local state, so memoizing here keeps mousemove cheap.
  const { totalCost, totalTokens, blendedRate, segments, hasData } = useMemo(() => {
    const safeBreakdown = breakdown ?? { uncached_input_cost_usd: 0, cache_read_cost_usd: 0, cache_write_cost_usd: 0, output_cost_usd: 0, total_cost_usd: 0, cost_available: true };
    const nextTotalCost = toNumber(safeBreakdown.total_cost_usd);
    const nextTotalTokens = rows.reduce((sum, row) => sum + row.total, 0);
    const segmentTokens = getCostSegmentTokens(rows);
    const tokenColors = getTokenColors(isDark);
    const baseSegments: CostBreakdownSegment[] = [
      { key: 'input', label: t('usage_stats.input_tokens'), value: toNumber(safeBreakdown.uncached_input_cost_usd), color: tokenColors.input, tokens: segmentTokens.input },
      { key: 'cacheRead', label: t('usage_stats.cache_read_tokens'), value: toNumber(safeBreakdown.cache_read_cost_usd), color: tokenColors.cacheRead, tokens: segmentTokens.cacheRead },
      { key: 'cacheWrite', label: t('usage_stats.cache_creation_tokens'), value: toNumber(safeBreakdown.cache_write_cost_usd), color: tokenColors.cacheWrite, tokens: segmentTokens.cacheWrite },
      { key: 'output', label: t('usage_stats.output_tokens'), value: toNumber(safeBreakdown.output_cost_usd), color: tokenColors.output, tokens: segmentTokens.output },
    ];
    const nextSegments = baseSegments.map((segment) => {
      const percent = nextTotalCost > 0 ? (segment.value / nextTotalCost) * 100 : 0;
      return {
        ...segment,
        percent,
        tooltipLines: [
          `${segment.label} · ${t('usage_stats.analysis_cost_share')}`,
          `${t('usage_stats.total_cost')}: ${formatUsd(segment.value)}`,
          `${t('usage_stats.analysis_cost_share')}: ${formatPercent(percent)}`,
          `${t('usage_stats.total_tokens')}: ${formatCompactNumber(segment.tokens)}`,
          `${t('usage_stats.analysis_cost_per_million_tokens')}: ${formatUsd(getCostRatePerMillion(segment.value, segment.tokens))}`,
        ],
      };
    });
    return {
      totalCost: nextTotalCost,
      totalTokens: nextTotalTokens,
      blendedRate: getCostRatePerMillion(nextTotalCost, nextTotalTokens),
      segments: nextSegments,
      hasData: rows.length > 0 || nextTotalCost > 0 || nextSegments.some((segment) => segment.value > 0),
    };
  }, [breakdown, rows, isDark, t]);
  const showCostTooltip = (lines: string[], event: MouseEvent<HTMLSpanElement> | FocusEvent<HTMLSpanElement>) => {
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
      <AnalysisCardHeader
        title={t('usage_stats.analysis_cost_breakdown_title')}
        subtitle={t('usage_stats.analysis_cost_breakdown_subtitle')}
      />
      {loading ? (
        <div className={styles.emptyState}>{t('common.loading')}</div>
      ) : !hasData ? (
        <div className={styles.emptyState}>{t('usage_stats.no_data')}</div>
      ) : (
        <div className={styles.costBreakdownBody}>
          <div className={styles.costStack} aria-label={t('usage_stats.analysis_cost_breakdown_title')}>
            {segments.map((segment) => (
              <span
                key={segment.key}
                className={styles.costStackSegment}
                style={{
                  '--cost-segment-color': segment.color,
                  flexBasis: `${Math.max(segment.percent, segment.value > 0 ? 4 : 0)}%`,
                } as CSSProperties}
                tabIndex={0}
                aria-label={segment.tooltipLines.join(', ')}
                onMouseEnter={(event) => showCostTooltip(segment.tooltipLines, event)}
                onMouseMove={(event) => showCostTooltip(segment.tooltipLines, event)}
                onMouseLeave={hideCostTooltip}
                onFocus={(event) => showCostTooltip(segment.tooltipLines, event)}
                onBlur={hideCostTooltip}
              >
                <span>{formatPercent(segment.percent)}</span>
              </span>
            ))}
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
              <span>{t('usage_stats.total_tokens')}</span>
              <strong>{formatCompactNumber(totalTokens)}</strong>
            </div>
            <div className={styles.costRateMetric}>
              <span>{t('usage_stats.total_cost')}</span>
              <strong>{formatUsd(totalCost)}</strong>
            </div>
            <div className={styles.costRateMetric}>
              <span>{t('usage_stats.analysis_cost_per_million_tokens')}</span>
              <strong>{formatUsd(blendedRate)}</strong>
              <small>{t('usage_stats.analysis_blended_rate')}</small>
            </div>
          </div>
          <div className={styles.costMetricGrid}>
            {segments.map((segment) => (
              <div key={segment.key} className={styles.costMetric}>
                <span className={styles.costMetricDot} style={{ backgroundColor: segment.color }} />
                <span className={styles.costMetricLabel}>{segment.label}</span>
                <strong>{formatUsd(segment.value)}</strong>
                <small>{formatPercent(segment.percent)}</small>
              </div>
            ))}
          </div>
        </div>
      )}
    </section>
  );
}

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

const getModelEfficiencyRate = (row: AnalysisModelEfficiencyItem) => getCostRatePerMillion(toNumber(row.cost_usd), toNumber(row.total_tokens));

function buildModelEfficiencyConfig({
  rows,
  labels,
  chartTheme,
  isDark,
  isMobile,
}: {
  rows: AnalysisModelEfficiencyItem[];
  labels: { totalTokens: string; costPerMillion: string; requests: string };
  chartTheme: KeeperChartTheme;
  isDark: boolean;
  isMobile: boolean;
}): { config: BaseConfig; data: EfficiencyPoint[] } {
  const pointRadii = buildModelEfficiencyRadii(rows.map((row) => toNumber(row.requests)));
  const data: EfficiencyPoint[] = rows.map((row, index) => ({
    model: row.model,
    requests: toNumber(row.requests),
    cost: toNumber(row.cost_usd),
    totalTokens: toNumber(row.total_tokens),
    costPerMillion: getModelEfficiencyRate(row),
    cacheReadRate: toNumber(row.cache_read_rate),
    radius: pointRadii[index],
    hoverRadius: Math.min(MODEL_EFFICIENCY_MAX_RADIUS + MODEL_EFFICIENCY_HOVER_RADIUS_DELTA, pointRadii[index] + MODEL_EFFICIENCY_HOVER_RADIUS_DELTA),
    color: getStableEntityColor(row.model, isDark),
  }));
  const xBounds = getLogScaleBounds(data.map((point) => point.totalTokens));
  const yBounds = getLogScaleBounds(data.map((point) => point.costPerMillion));
  const config: BaseConfig = {
    autoFit: true,
    animate: false,
    legend: false,
    theme: { type: isDark ? 'classicDark' : 'classic' },
    scale: {
      x: {
        type: 'log',
        ...(xBounds.min ? { domainMin: xBounds.min, domainMax: xBounds.max } : {}),
        tickCount: isMobile ? 4 : 5,
        nice: false,
      },
      y: {
        type: 'log',
        ...(yBounds.min ? { domainMin: yBounds.min, domainMax: yBounds.max } : {}),
        tickCount: isMobile ? 4 : 5,
        nice: false,
      },
      color: {
        domain: data.map((point) => point.model),
        range: data.map((point) => point.color),
      },
      size: { type: 'identity' },
    },
    axis: {
      x: {
        title: labels.totalTokens,
        grid: true,
        tickCount: isMobile ? 4 : 5,
        labelFormatter: (value: number) => formatCompactNumber(Number(value)),
        labelFill: chartTheme.textSecondary,
        labelFontSize: 10,
        titleFill: chartTheme.textSecondary,
        gridStroke: chartTheme.grid,
        line: true,
        lineStroke: chartTheme.axis,
        tickStroke: chartTheme.axis,
      },
      y: {
        title: labels.costPerMillion,
        grid: true,
        tickCount: isMobile ? 4 : 5,
        labelFormatter: (value: number) => formatUsd(Number(value)),
        labelFill: chartTheme.textSecondary,
        labelFontSize: 10,
        titleFill: chartTheme.textSecondary,
        gridStroke: chartTheme.grid,
        line: true,
        lineStroke: chartTheme.axis,
        tickStroke: chartTheme.axis,
      },
    },
    interaction: {
      tooltip: { shared: false, crosshairs: true, marker: true },
    },
    children: [{
      type: 'point',
      data,
      encode: { x: 'totalTokens', y: 'costPerMillion', color: 'model', size: 'radius' },
      style: {
        r: (datum: EfficiencyPoint) => datum.radius,
        fill: (datum: EfficiencyPoint) => datum.color,
        fillOpacity: 0.86,
        stroke: chartTheme.tooltipBackground,
        lineWidth: 2,
      },
      state: {
        active: { r: (datum: EfficiencyPoint) => datum.hoverRadius, lineWidth: 3 },
      },
      tooltip: {
        title: (datum: EfficiencyPoint) => datum.model,
        items: [
          (datum: EfficiencyPoint) => ({ name: labels.totalTokens, color: datum.color, value: formatCompactNumber(datum.totalTokens) }),
          (datum: EfficiencyPoint) => ({ name: labels.costPerMillion, color: datum.color, value: formatUsd(datum.costPerMillion) }),
          (datum: EfficiencyPoint) => ({ name: labels.requests, color: datum.color, value: formatCompactNumber(datum.requests) }),
        ],
      },
    }],
  };
  return { config, data };
}

function ModelEfficiencyTable({ data, labels }: { data: EfficiencyPoint[]; labels: { model: string; totalTokens: string; costPerMillion: string; requests: string } }) {
  return (
    <table className={styles.chartDataTable}>
      <caption>{labels.costPerMillion}</caption>
      <thead>
        <tr>
          <th scope="col">{labels.model}</th>
          <th scope="col">{labels.totalTokens}</th>
          <th scope="col">{labels.costPerMillion}</th>
          <th scope="col">{labels.requests}</th>
        </tr>
      </thead>
      <tbody>
        {data.map((point) => (
          <tr key={point.model}>
            <th scope="row">{point.model}</th>
            <td>{formatCompactNumber(point.totalTokens)}</td>
            <td>{formatUsd(point.costPerMillion)}</td>
            <td>{formatCompactNumber(point.requests)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function ModelEfficiencyCard({ rows, loading, isDark, isMobile }: { rows: AnalysisModelEfficiencyItem[]; loading: boolean; isDark: boolean; isMobile: boolean }) {
  const { t } = useTranslation();
  const chartTheme = useMemo(() => getChartTheme(isDark), [isDark]);
  const pricedRows = useMemo(
    () => rows.filter((row) => row.cost_available !== false && toNumber(row.total_tokens) > 0 && getModelEfficiencyRate(row) > 0),
    [rows],
  );
  const labels = useMemo(() => ({
    model: t('usage_stats.model_name'),
    totalTokens: t('usage_stats.total_tokens'),
    costPerMillion: t('usage_stats.analysis_cost_per_million_tokens'),
    requests: t('usage_stats.requests_count'),
  }), [t]);
  const chart = useMemo(() => buildModelEfficiencyConfig({ rows: pricedRows, labels, chartTheme, isDark, isMobile }), [chartTheme, isDark, isMobile, labels, pricedRows]);
  const legendItems = useMemo(() => chart.data.map((point) => ({ label: point.model, color: point.color })), [chart.data]);
  const hasData = rows.length > 0;
  const hasPricedData = pricedRows.length > 0;
  return (
    <section className={`${styles.analysisCard} ${styles.modelEfficiencyCard}`}>
      <AnalysisCardHeader
        title={t('usage_stats.analysis_model_efficiency_title')}
        subtitle={t('usage_stats.analysis_model_efficiency_subtitle')}
      />
      {loading ? (
        <div className={styles.emptyState}>{t('common.loading')}</div>
      ) : !hasData ? (
        <div className={styles.emptyState}>{t('usage_stats.no_data')}</div>
      ) : (
        <div className={styles.modelEfficiencyBody}>
          {hasPricedData ? (
            <>
              <ChartLegend label={t('usage_stats.analysis_model_efficiency_title')} items={legendItems} />
              <div className={styles.efficiencyChartFrame}>
                <Base {...chart.config} />
              </div>
              <ModelEfficiencyTable data={chart.data} labels={labels} />
            </>
          ) : (
            <div className={styles.emptyState}>{t('usage_stats.no_data')}</div>
          )}
        </div>
      )}
    </section>
  );
}

type HeatmapCellView = { key: string; background: string; color: string; tokensLabel: string; tooltipLines: string[] };
type HeatmapRowView = { apiKey: string; apiKeyLabel: string; cells: HeatmapCellView[] };
type HeatmapModelHeaderView = { model: string; tooltipLines: string[] };
type HeatmapGridView = { modelHeaders: HeatmapModelHeaderView[]; rows: HeatmapRowView[] };
type HeatmapTooltipHandler = (lines: string[], event: MouseEvent<HTMLDivElement> | FocusEvent<HTMLDivElement>) => void;

// Memoized so hovering (which only updates the parent's tooltip state) does not
// re-render the whole cell grid; props are precomputed data + stable handlers.
const HeatmapGrid = memo(function HeatmapGrid({
  grid,
  cornerLabel,
  onShowTooltip,
  onHideTooltip,
}: {
  grid: HeatmapGridView;
  cornerLabel: string;
  onShowTooltip: HeatmapTooltipHandler;
  onHideTooltip: () => void;
}) {
  return (
    <div className={styles.heatmapScroller}>
      <div className={styles.heatmapGrid} style={{ gridTemplateColumns: `150px repeat(${grid.modelHeaders.length}, minmax(82px, 1fr))` }}>
        <div className={styles.heatmapCorner}>{cornerLabel}</div>
        {grid.modelHeaders.map((header) => (
          <div
            key={header.model}
            className={`${styles.heatmapHeaderCell} ${styles.heatmapModelHeaderCell}`}
            data-full-name={header.model}
            tabIndex={0}
            aria-label={header.model}
            onMouseEnter={(event) => onShowTooltip(header.tooltipLines, event)}
            onMouseMove={(event) => onShowTooltip(header.tooltipLines, event)}
            onMouseLeave={onHideTooltip}
            onFocus={(event) => onShowTooltip(header.tooltipLines, event)}
            onBlur={onHideTooltip}
          >
            <span className={`${styles.heatmapTruncatedLabel} ${styles.heatmapModelLabel}`}>{header.model}</span>
          </div>
        ))}
        {grid.rows.map((row) => (
          <div key={row.apiKey} className={styles.heatmapRowContents}>
            <div className={`${styles.heatmapRowLabel} ${styles.heatmapTooltipTarget}`} data-full-name={row.apiKeyLabel}>
              <span className={styles.heatmapTruncatedLabel}>{row.apiKeyLabel}</span>
            </div>
            {row.cells.map((cell) => (
              <div
                key={cell.key}
                className={styles.heatmapCell}
                style={{ background: cell.background, color: cell.color } as CSSProperties}
                tabIndex={0}
                aria-label={cell.tooltipLines.join(', ')}
                data-tooltip={cell.tooltipLines.join('\n')}
                onMouseEnter={(event) => onShowTooltip(cell.tooltipLines, event)}
                onMouseMove={(event) => onShowTooltip(cell.tooltipLines, event)}
                onMouseLeave={onHideTooltip}
                onFocus={(event) => onShowTooltip(cell.tooltipLines, event)}
                onBlur={onHideTooltip}
              >
                <span className={styles.heatmapCellTokenValue}>{cell.tokensLabel}</span>
              </div>
            ))}
          </div>
        ))}
      </div>
    </div>
  );
});

function Heatmap({ cells, apiKeys, apiKeyLabels, models, loading, isDark }: { cells: AnalysisHeatmapCell[]; apiKeys: string[]; apiKeyLabels: Record<string, string>; models: string[]; loading: boolean; isDark: boolean }) {
  const { t } = useTranslation();
  const [tooltip, setTooltip] = useState<FloatingTooltipState | null>(null);
  // Precompute the whole grid (colors, formatted tokens, tooltip lines) once per
  // data/theme change. Hovering only updates local `tooltip` state, and the grid
  // itself is a memoized child, so mousemove no longer recomputes 300 cells.
  const grid = useMemo<HeatmapGridView>(() => {
    const cellMap = new Map(cells.map((cell) => [`${cell.api_key}\0${cell.model}`, cell]));
    const maxHeatmapTokens = cells.reduce((max, cell) => Math.max(max, toNumber(cell.total_tokens)), 0);
    const getAPIKeyLabel = (apiKey: string) => apiKeyLabels[apiKey] || apiKey;
    const buildTooltipLines = (apiKey: string, model: string, cell: AnalysisHeatmapCell | undefined) => [
      `${getAPIKeyLabel(apiKey)} / ${model}`,
      `${t('usage_stats.requests_count')}: ${formatCompactNumber(toNumber(cell?.requests))}`,
      `${t('usage_stats.input_tokens')}: ${formatCompactNumber(toNumber(cell?.input_tokens))}`,
      `${t('usage_stats.output_tokens')}: ${formatCompactNumber(toNumber(cell?.output_tokens))}`,
      `${t('usage_stats.reasoning_tokens')}: ${formatCompactNumber(toNumber(cell?.reasoning_tokens))}`,
      `${t('usage_stats.cache_read_tokens')}: ${formatCompactNumber(toNumber(cell?.cache_read_tokens))}`,
      `${t('usage_stats.cache_creation_tokens')}: ${formatCompactNumber(toNumber(cell?.cache_creation_tokens))}`,
      `${t('usage_stats.total_tokens')}: ${formatCompactNumber(toNumber(cell?.total_tokens))}`,
      `${t('usage_stats.total_cost')}: ${formatUsd(toNumber(cell?.cost_usd))}`,
    ];
    return {
      modelHeaders: models.map((model) => ({ model, tooltipLines: [model] })),
      rows: apiKeys.map((apiKey) => ({
        apiKey,
        apiKeyLabel: getAPIKeyLabel(apiKey),
        cells: models.map((model) => {
          const cell = cellMap.get(`${apiKey}\0${model}`);
          const heatmapTokens = toNumber(cell?.total_tokens);
          const intensity = getHeatmapVisualIntensity(heatmapTokens, maxHeatmapTokens);
          return {
            key: `${apiKey}-${model}`,
            background: getHeatmapCellColor(intensity, isDark),
            color: getHeatmapCellTextColor(intensity, isDark),
            tokensLabel: formatCompactNumber(heatmapTokens),
            tooltipLines: buildTooltipLines(apiKey, model, cell),
          };
        }),
      })),
    };
  }, [cells, apiKeys, apiKeyLabels, models, isDark, t]);
  const showTooltip = useCallback((lines: string[], event: MouseEvent<HTMLDivElement> | FocusEvent<HTMLDivElement>) => {
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
  }, []);
  const hideTooltip = useCallback(() => setTooltip(null), []);

  return (
    <section className={`${styles.analysisCard} ${styles.heatmapCard} ${isDark ? styles.heatmapCardDark : styles.heatmapCardLight}`}>
      <AnalysisCardHeader
        title={t('usage_stats.analysis_heatmap_title')}
        subtitle={t('usage_stats.analysis_heatmap_subtitle')}
      />
      {loading ? (
        <div className={styles.emptyState}>{t('common.loading')}</div>
      ) : cells.length === 0 ? (
        <div className={styles.emptyState}>{t('usage_stats.no_data')}</div>
      ) : (
        <>
          <div className={styles.analysisChartSurface}>
            <HeatmapGrid
              grid={grid}
              cornerLabel={t('usage_stats.analysis_heatmap_api_key')}
              onShowTooltip={showTooltip}
              onHideTooltip={hideTooltip}
            />
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

/**
 * Defers mounting heavy G2 canvas charts until they scroll near the viewport so
 * the six analysis charts do not all initialize on the same frame (the main
 * cause of the slow tab open). Renders eagerly where IntersectionObserver is
 * unavailable (SSR / tests) so markup and unit tests are unaffected.
 */
function DeferredChart({ children, minHeight }: { children: ReactNode; minHeight: number }) {
  const [visible, setVisible] = useState(() => typeof IntersectionObserver === 'undefined');
  const placeholderRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    if (visible || typeof IntersectionObserver === 'undefined') return undefined;
    const node = placeholderRef.current;
    if (!node) return undefined;
    const observer = new IntersectionObserver((entries) => {
      if (entries.some((entry) => entry.isIntersecting)) {
        setVisible(true);
        observer.disconnect();
      }
    }, { rootMargin: '200px' });
    observer.observe(node);
    return () => observer.disconnect();
  }, [visible]);

  if (visible) return <>{children}</>;
  return <div ref={placeholderRef} style={{ minHeight }} aria-hidden="true" />;
}

export function getUnpricedAnalysisModels(analysis: AnalysisResponse | null): string[] {
  if (!analysis) return [];
  return normalizeUnpricedModels([
    ...(analysis.model_efficiency ?? [])
      .filter((item) => item.cost_available === false)
      .map((item) => item.model),
    ...(analysis.model_composition ?? [])
      .filter((item) => item.cost_available === false)
      .map((item) => item.label || item.key),
    ...(analysis.heatmap?.cells ?? [])
      .filter((cell) => cell.cost_available === false)
      .map((cell) => cell.model),
  ]);
}

export function AnalysisPanel({ analysis, loading, isDark, isMobile, onConfigurePricing }: AnalysisPanelProps) {
  const { t } = useTranslation();
  const tokenRows = useMemo(
    () => buildTokenUsageRows(analysis?.token_usage ?? [], analysis?.granularity ?? 'hourly', analysis?.timezone),
    [analysis],
  );
  const apiComposition = useMemo(() => takeMajorComposition(analysis?.api_key_composition ?? [], t('usage_stats.analysis_others')), [analysis, t]);
  const modelComposition = useMemo(() => takeMajorComposition(analysis?.model_composition ?? [], t('usage_stats.analysis_others')), [analysis, t]);
  const authFilesComposition = useMemo(() => takeMajorComposition(analysis?.auth_files_composition ?? [], t('usage_stats.analysis_others')), [analysis, t]);
  const aiProviderComposition = useMemo(() => takeMajorComposition(analysis?.ai_provider_composition ?? [], t('usage_stats.analysis_others')), [analysis, t]);
  const analysisWindowMinutes = useMemo(() => calculateAnalysisWindowMinutes(analysis), [analysis]);
  const unpricedModels = useMemo(() => getUnpricedAnalysisModels(analysis), [analysis]);
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
        <CostBreakdownCard breakdown={analysis?.cost_breakdown} rows={tokenRows} loading={loading} isDark={isDark} />
        <ModelEfficiencyCard rows={analysis?.model_efficiency ?? []} loading={loading} isDark={isDark} isMobile={isMobile} />
      </div>
      <DeferredChart minHeight={360}>
        <LatencyDiagnosticsCard diagnostics={analysis?.latency_diagnostics} loading={loading} isDark={isDark} isMobile={isMobile} />
      </DeferredChart>
      <DeferredChart minHeight={360}>
        <CompositionPanel tabs={compositionTabs} loading={loading} isDark={isDark} windowMinutes={analysisWindowMinutes} />
      </DeferredChart>
      <DeferredChart minHeight={360}>
        <Heatmap
          cells={analysis?.heatmap?.cells ?? []}
          apiKeys={analysis?.heatmap?.api_keys ?? []}
          apiKeyLabels={analysis?.heatmap?.api_key_labels ?? {}}
          models={analysis?.heatmap?.models ?? []}
          loading={loading}
          isDark={isDark}
        />
      </DeferredChart>
      <PricingCoverageNotice models={unpricedModels} onConfigure={onConfigurePricing} />
    </div>
  );
}
