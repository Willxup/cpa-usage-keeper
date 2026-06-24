import { useEffect, useMemo, useState, type CSSProperties, type FocusEvent, type MouseEvent } from 'react';
import { useTranslation } from 'react-i18next';
import '@/lib/chartjs';
import type { Chart, ChartData, ChartOptions, Plugin, ScriptableContext, TooltipModel } from 'chart.js';
import { Bar, Doughnut, Scatter } from 'react-chartjs-2';
import type { AnalysisCompositionItem, AnalysisCostBreakdown, AnalysisHeatmapCell, AnalysisLatencyDiagnostics, AnalysisModelEfficiencyItem, AnalysisResponse, AnalysisTokenUsageBucket } from '@/lib/types';
import { calculateDisplayInputTokens, calculateDisplayOutputTokens, formatCompactNumber, formatDurationMs, formatUsd } from '@/utils/usage';
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
  averageLine: string;
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
type TokenAverageLinePluginOptions = {
  value: number;
  color: string;
};
type TokenChartOptions = ChartOptions<'bar'> & {
  plugins?: ChartOptions<'bar'>['plugins'] & {
    analysisTokenAverageLine?: TokenAverageLinePluginOptions;
  };
};
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
type CostRatePoint = {
  label: string;
  rate: number;
  cost: number;
  tokens: number;
};
type ModelEfficiencyColor = {
  base: string;
  light: string;
  dark: string;
};
type LatencyScatterPoint = {
  x: number;
  y: number;
};
type LatencyDiagnosticsPluginLabels = {
  p95TTFT: string;
  p95Latency: string;
};
type LatencyThemeColors = {
  point: string;
  pointFill: string;
  p95TTFT: string;
  p95Latency: string;
};
type LatencyDiagnosticsPluginOptions = {
  p95TTFTMS: number;
  p95LatencyMS: number;
  labels: LatencyDiagnosticsPluginLabels;
  colors: LatencyThemeColors;
};
type LatencyReferenceHover = {
  kind: 'ttft' | 'latency';
  text: string;
  x: number;
  y: number;
  color: string;
};
type LatencyPluginEventArgs = Parameters<NonNullable<Plugin<'scatter'>['afterEvent']>>[1];

const CHART_COLORS: GradientColor[] = [
  { base: '#1d4ed8', light: '#60a5fa' },
  { base: '#ca8a04', light: '#facc15' },
  { base: '#15803d', light: '#22c55e' },
  { base: '#7e22ce', light: '#c084fc' },
  { base: '#b91c1c', light: '#ef4444' },
  { base: '#0891b2', light: '#67e8f9' },
];
const TOKEN_COLORS = {
  input: { base: '#2563eb', light: '#93c5fd' },
  output: { base: '#16a34a', light: '#86efac' },
  cached: { base: '#d97706', light: '#fde68a' },
  reasoning: { base: '#8b5cf6', light: '#d8b4fe' },
  requests: '#ff5a40',
  cost: '#14b8a6',
};
const LATENCY_COLORS = {
  light: {
    point: '#14b8a6',
    pointFill: 'rgba(45, 212, 191, 0.62)',
    p95TTFT: '#38bdf8',
    p95Latency: '#fb7185',
  },
  dark: {
    point: '#5eead4',
    pointFill: 'rgba(94, 234, 212, 0.72)',
    p95TTFT: '#7dd3fc',
    p95Latency: '#fda4af',
  },
} satisfies Record<'light' | 'dark', LatencyThemeColors>;
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
const LATENCY_REFERENCE_HIT_RADIUS_PX = 8;
const EMPTY_COMPOSITION_ITEMS: AnalysisCompositionItem[] = [];
const latencyReferenceHoverStates = new WeakMap<Chart<'scatter'>, LatencyReferenceHover>();
type TokenLabels = {
  input: string;
  output: string;
  cached: string;
  reasoning: string;
  total: string;
  average: string;
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

const getTokenAverageLinePluginOptions = (chart: Chart<'bar'>): TokenAverageLinePluginOptions | undefined => {
  const plugins = chart.options.plugins as (ChartOptions<'bar'>['plugins'] & { analysisTokenAverageLine?: TokenAverageLinePluginOptions }) | undefined;
  return plugins?.analysisTokenAverageLine;
};

const drawTokenAverageLinePlugin: Plugin<'bar'> = {
  id: 'analysis-token-average-line',
  afterDatasetsDraw: (chart) => {
    const options = getTokenAverageLinePluginOptions(chart);
    if (!options || !Number.isFinite(options.value)) return;
    const yScale = chart.scales.tokens;
    if (!yScale) return;
    const { ctx, chartArea } = chart;
    if (!chartArea) return;
    const y = yScale.getPixelForValue(options.value);
    if (!Number.isFinite(y) || y < chartArea.top || y > chartArea.bottom) return;

    ctx.save();
    // 平均线是参考基准，覆盖在数据层上方但使用弱色，避免被误读为新的业务趋势。
    ctx.strokeStyle = options.color;
    ctx.lineWidth = 1.5;
    ctx.setLineDash([5, 5]);
    ctx.beginPath();
    ctx.moveTo(chartArea.left, y);
    ctx.lineTo(chartArea.right, y);
    ctx.stroke();
    ctx.restore();
  },
};

const getChartTheme = (isDark: boolean): ChartTheme => ({
  textPrimary: isDark ? '#f5f1e8' : '#111827',
  textSecondary: isDark ? 'rgba(255, 255, 255, 0.72)' : 'rgba(17, 24, 39, 0.72)',
  grid: isDark ? 'rgba(255, 255, 255, 0.06)' : 'rgba(17, 24, 39, 0.06)',
  axis: isDark ? 'rgba(255, 255, 255, 0.10)' : 'rgba(17, 24, 39, 0.10)',
  averageLine: isDark ? 'rgba(203, 213, 225, 0.62)' : 'rgba(71, 85, 105, 0.62)',
  tooltipBg: isDark ? 'rgba(17, 24, 39, 0.94)' : 'rgba(255, 255, 255, 0.98)',
  tooltipBorder: isDark ? 'rgba(255, 255, 255, 0.10)' : 'rgba(17, 24, 39, 0.10)',
  tooltipBody: isDark ? 'rgba(255, 255, 255, 0.86)' : '#374151',
});

const getLatencyColors = (isDark: boolean): LatencyThemeColors => (isDark ? LATENCY_COLORS.dark : LATENCY_COLORS.light);

const getLatencyDiagnosticsPluginOptions = (chart: Chart<'scatter'>): LatencyDiagnosticsPluginOptions | undefined => {
  const plugins = chart.options.plugins as (ChartOptions<'scatter'>['plugins'] & { analysisLatencyDiagnostics?: LatencyDiagnosticsPluginOptions }) | undefined;
  return plugins?.analysisLatencyDiagnostics;
};

const drawLatencyReferenceLabel = (
  chart: Chart<'scatter'>,
  text: string,
  x: number,
  y: number,
  color: string,
  align: CanvasTextAlign,
) => {
  const { ctx, chartArea } = chart;
  ctx.save();
  ctx.font = '700 11px Inter, system-ui, sans-serif';
  ctx.textAlign = align;
  ctx.textBaseline = 'middle';
  ctx.fillStyle = color;
  const labelX = Math.max(chartArea.left + 6, Math.min(x, chartArea.right - 6));
  const labelY = Math.max(chartArea.top + 10, Math.min(y, chartArea.bottom - 10));
  ctx.fillText(text, labelX, labelY);
  ctx.restore();
};

const getBoundedHoverPoint = (value: number, min: number, max: number) => Math.max(min, Math.min(value, max));

const setLatencyReferenceHover = (
  chart: Chart<'scatter'>,
  args: LatencyPluginEventArgs,
  hover: LatencyReferenceHover | undefined,
) => {
  const previous = latencyReferenceHoverStates.get(chart);
  const changed = previous?.text !== hover?.text
    || Math.round(previous?.x ?? -1) !== Math.round(hover?.x ?? -1)
    || Math.round(previous?.y ?? -1) !== Math.round(hover?.y ?? -1);
  if (!changed) return;

  if (hover) {
    latencyReferenceHoverStates.set(chart, hover);
  } else {
    latencyReferenceHoverStates.delete(chart);
  }
  if (chart.canvas) {
    chart.canvas.style.cursor = '';
  }
  args.changed = true;
};

const getLatencyReferenceHover = (
  chart: Chart<'scatter'>,
  event: LatencyPluginEventArgs['event'],
  options: LatencyDiagnosticsPluginOptions,
): LatencyReferenceHover | undefined => {
  if (event.x == null || event.y == null) return undefined;
  const xScale = chart.scales.x;
  const yScale = chart.scales.y;
  if (!xScale || !yScale) return undefined;
  const { chartArea } = chart;
  if (!chartArea) return undefined;
  const hovers: Array<LatencyReferenceHover & { distance: number }> = [];

  if (options.p95TTFTMS > 0) {
    const x = xScale.getPixelForValue(options.p95TTFTMS);
    const distance = Math.abs(event.x - x);
    if (x >= chartArea.left && x <= chartArea.right && distance <= LATENCY_REFERENCE_HIT_RADIUS_PX) {
      hovers.push({
        kind: 'ttft',
        text: `${options.labels.p95TTFT}: ${formatDurationMs(options.p95TTFTMS)}`,
        x,
        y: getBoundedHoverPoint(event.y, chartArea.top, chartArea.bottom),
        color: options.colors.p95TTFT,
        distance,
      });
    }
  }

  if (options.p95LatencyMS > 0) {
    const y = yScale.getPixelForValue(options.p95LatencyMS);
    const distance = Math.abs(event.y - y);
    if (y >= chartArea.top && y <= chartArea.bottom && distance <= LATENCY_REFERENCE_HIT_RADIUS_PX) {
      hovers.push({
        kind: 'latency',
        text: `${options.labels.p95Latency}: ${formatDurationMs(options.p95LatencyMS)}`,
        x: getBoundedHoverPoint(event.x, chartArea.left, chartArea.right),
        y,
        color: options.colors.p95Latency,
        distance,
      });
    }
  }

  hovers.sort((left, right) => left.distance - right.distance);
  return hovers[0];
};

const drawLatencyReferenceHover = (chart: Chart<'scatter'>, hover: LatencyReferenceHover) => {
  const { ctx, chartArea } = chart;
  if (!chartArea) return;
  const paddingX = 8;
  const height = 24;
  const gap = 12;
  ctx.save();
  ctx.font = '700 11px Inter, system-ui, sans-serif';
  const width = Math.ceil(ctx.measureText(hover.text).width) + paddingX * 2;
  let x = hover.x + gap;
  if (x + width > chartArea.right - 4) {
    x = hover.x - width - gap;
  }
  x = getBoundedHoverPoint(x, chartArea.left + 4, chartArea.right - width - 4);
  let y = hover.y - height - gap;
  if (y < chartArea.top + 4) {
    y = hover.y + gap;
  }
  y = getBoundedHoverPoint(y, chartArea.top + 4, chartArea.bottom - height - 4);
  ctx.fillStyle = 'rgba(17, 24, 39, 0.94)';
  ctx.strokeStyle = hover.color;
  ctx.lineWidth = 1;
  ctx.fillRect(x, y, width, height);
  ctx.strokeRect(x, y, width, height);
  ctx.fillStyle = '#ffffff';
  ctx.textAlign = 'left';
  ctx.textBaseline = 'middle';
  ctx.fillText(hover.text, x + paddingX, y + height / 2);
  ctx.restore();
};

const latencyDiagnosticsPlugin: Plugin<'scatter'> = {
  id: 'analysis-latency-diagnostics',
  afterEvent: (chart, args) => {
    const options = getLatencyDiagnosticsPluginOptions(chart);
    if (!options) return;
    if (args.event.type === 'mouseout' || !args.inChartArea) {
      setLatencyReferenceHover(chart, args, undefined);
      return;
    }
    if (args.event.type !== 'mousemove') return;
    setLatencyReferenceHover(chart, args, getLatencyReferenceHover(chart, args.event, options));
  },
  afterDatasetsDraw: (chart) => {
    const options = getLatencyDiagnosticsPluginOptions(chart);
    if (!options) return;
    const xScale = chart.scales.x;
    const yScale = chart.scales.y;
    if (!xScale || !yScale) return;
    const { ctx, chartArea } = chart;
    if (!chartArea) return;
    ctx.save();
    // p95 参考线覆盖在样本点上，辅助快速区分首字慢和总耗时慢。
    const hover = latencyReferenceHoverStates.get(chart);
    if (options.p95TTFTMS > 0) {
      const x = xScale.getPixelForValue(options.p95TTFTMS);
      if (x >= chartArea.left && x <= chartArea.right) {
        const active = hover?.kind === 'ttft';
        ctx.lineWidth = active ? 2.6 : 1.4;
        ctx.setLineDash(active ? [4, 3] : [5, 5]);
        ctx.strokeStyle = options.colors.p95TTFT;
        ctx.beginPath();
        ctx.moveTo(x, chartArea.top);
        ctx.lineTo(x, chartArea.bottom);
        ctx.stroke();
        drawLatencyReferenceLabel(chart, options.labels.p95TTFT, x + 6, chartArea.top + 16, options.colors.p95TTFT, 'left');
      }
    }
    if (options.p95LatencyMS > 0) {
      const y = yScale.getPixelForValue(options.p95LatencyMS);
      if (y >= chartArea.top && y <= chartArea.bottom) {
        const active = hover?.kind === 'latency';
        ctx.lineWidth = active ? 2.6 : 1.4;
        ctx.setLineDash(active ? [4, 3] : [5, 5]);
        ctx.strokeStyle = options.colors.p95Latency;
        ctx.beginPath();
        ctx.moveTo(chartArea.left, y);
        ctx.lineTo(chartArea.right, y);
        ctx.stroke();
        drawLatencyReferenceLabel(chart, options.labels.p95Latency, chartArea.right - 6, y - 12, options.colors.p95Latency, 'right');
      }
    }
    if (hover) {
      drawLatencyReferenceHover(chart, hover);
    }
    ctx.restore();
  },
};

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
        { at: 0, color: [26, 24, 23] },
        { at: 0.25, color: [55, 50, 47] },
        { at: 0.5, color: [90, 83, 76] },
        { at: 0.75, color: [130, 120, 112] },
        { at: 1, color: [178, 170, 163] },
      ] satisfies Array<{ at: number; color: [number, number, number] }>
      : [
        { at: 0, color: [240, 238, 232] },
        { at: 0.2, color: [206, 200, 192] },
        { at: 0.45, color: [173, 165, 156] },
        { at: 0.7, color: [139, 131, 124] },
        { at: 1, color: [92, 82, 74] },
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
    return clampedIntensity > 0.58 ? '#f5f3ef' : '#3a3530';
  }
  return clampedIntensity > 0.75 ? '#1a1817' : '#f0eee8';
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
  // Analysis bucket 已按项目 TZ 聚合，前端必须显式使用响应 TZ，避免被 CI 或浏览器本地时区二次换算。
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

function calculateAverageTotalTokens(rows: ChartRow[]): number {
  if (rows.length === 0) return 0;
  return rows.reduce((sum, row) => sum + row.total, 0) / rows.length;
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

function buildTokenLegendItems(labels: TokenLabels, averageTokenTotal: number, averageLineColor: string): LegendItem[] {
  return [
    { label: labels.input, color: TOKEN_COLORS.input.base },
    { label: labels.output, color: TOKEN_COLORS.output.base },
    { label: labels.cached, color: TOKEN_COLORS.cached.base },
    { label: labels.reasoning, color: TOKEN_COLORS.reasoning.base },
    { label: labels.requests, color: TOKEN_COLORS.requests },
    { label: labels.cost, color: TOKEN_COLORS.cost },
    { label: `${labels.average}: ${formatCompactNumber(averageTokenTotal)}`, color: averageLineColor },
  ];
}

function buildAnalysisTokenChartOptions({ chartTheme, isMobile, totalTokens, totalLabel, averageTokenTotal }: { chartTheme: ChartTheme; isMobile: boolean; totalTokens: number[]; totalLabel: string; averageTokenTotal: number }): TokenChartOptions {
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
        footerColor: chartTheme.tooltipBody,
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
      analysisTokenAverageLine: {
        value: averageTokenTotal,
        color: chartTheme.averageLine,
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
        data: rows.map((row) => row.cost),
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

function CostHeaderHint({ show, label }: { show: boolean; label: string }) {
  return show ? <small className={styles.costHeaderHint}>{label}</small> : null;
}

function AnalysisCardHeader({ title, subtitle, showPricingHint, hint }: { title: string; subtitle: string; showPricingHint: boolean; hint: string }) {
  return (
    <div className={styles.cardHeader}>
      <div className={styles.cardTitleLine}>
        <h2>{title}</h2>
        <CostHeaderHint show={showPricingHint} label={hint} />
      </div>
      <p>{subtitle}</p>
    </div>
  );
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
    average: t('usage_stats.analysis_token_average'),
    requests: t('usage_stats.requests_count'),
    cost: t('usage_stats.total_cost'),
  }), [t]);
  const chartTheme = useMemo(() => getChartTheme(isDark), [isDark]);
  const averageTokenTotal = useMemo(() => calculateAverageTotalTokens(rows), [rows]);
  const chartData = useMemo(() => buildAnalysisTokenChartData(rows, tokenLabels), [rows, tokenLabels]);
  const chartOptions = useMemo(() => buildAnalysisTokenChartOptions({
    chartTheme,
    isMobile,
    totalTokens: rows.map((row) => row.total),
    totalLabel: tokenLabels.total,
    averageTokenTotal,
  }), [averageTokenTotal, chartTheme, isMobile, rows, tokenLabels.total]);
  const legendItems = useMemo(() => buildTokenLegendItems(tokenLabels, averageTokenTotal, chartTheme.averageLine), [averageTokenTotal, chartTheme.averageLine, tokenLabels]);
  const hasUnavailableCost = rows.some((row) => !row.costAvailable);
  return (
    <section className={`${styles.analysisCard} ${styles.tokenUsageCard}`}>
      <AnalysisCardHeader
        title={t('usage_stats.analysis_token_usage_title')}
        subtitle={t('usage_stats.analysis_token_usage_subtitle')}
        showPricingHint={hasUnavailableCost}
        hint={t('usage_stats.cost_need_price')}
      />
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
            <Bar data={chartData} options={chartOptions} plugins={[drawRequestsLineOnTopPlugin, drawTokenAverageLinePlugin]} />
          </div>
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

function buildLatencyDiagnosticsChartData(diagnostics: AnalysisLatencyDiagnostics, label: string, colors: LatencyThemeColors): ChartData<'scatter', LatencyScatterPoint[], string> {
  return {
    labels: diagnostics.points.map((point) => `${point.ttft_ms}/${point.latency_ms}`),
    datasets: [{
      label,
      data: diagnostics.points.map((point) => ({
        x: toNumber(point.ttft_ms),
        y: toNumber(point.latency_ms),
      })),
      pointRadius: 3,
      pointHoverRadius: 5,
      pointBackgroundColor: colors.pointFill,
      pointBorderColor: 'transparent',
      pointBorderWidth: 0,
      pointHoverBorderWidth: 0,
      borderColor: 'transparent',
      borderWidth: 0,
      showLine: false,
      clip: false,
    }],
  };
}

function buildLatencyDiagnosticsChartOptions({
  diagnostics,
  chartTheme,
  isMobile,
  labels,
  colors,
}: {
  diagnostics: AnalysisLatencyDiagnostics;
  chartTheme: ChartTheme;
  isMobile: boolean;
  labels: {
    ttft: string;
    latency: string;
    p95TTFT: string;
    p95Latency: string;
  };
  colors: LatencyThemeColors;
}): ChartOptions<'scatter'> {
  const xBounds = getLatencyLogAxisBounds(getLatencyAxisValues(diagnostics, 'ttft'));
  const yBounds = getLatencyLogAxisBounds(getLatencyAxisValues(diagnostics, 'latency'));
  const plugins = {
    legend: { display: false },
    tooltip: {
      backgroundColor: chartTheme.tooltipBg,
      titleColor: chartTheme.textPrimary,
      bodyColor: chartTheme.tooltipBody,
      borderColor: chartTheme.tooltipBorder,
      borderWidth: 1,
      padding: 10,
      displayColors: false,
      callbacks: {
        title: () => [],
        label: (context) => [
          `${labels.ttft}: ${formatDurationMs(context.parsed.x)}`,
          `${labels.latency}: ${formatDurationMs(context.parsed.y)}`,
        ],
      },
    },
    analysisLatencyDiagnostics: {
      p95TTFTMS: toNumber(diagnostics.p95_ttft_ms),
      p95LatencyMS: toNumber(diagnostics.p95_latency_ms),
      labels: {
        p95TTFT: labels.p95TTFT,
        p95Latency: labels.p95Latency,
      },
      colors,
    },
  } as ChartOptions<'scatter'>['plugins'] & { analysisLatencyDiagnostics: LatencyDiagnosticsPluginOptions };

  return {
    responsive: true,
    maintainAspectRatio: false,
    interaction: { mode: 'nearest', intersect: false },
    layout: { padding: { top: 18, right: 14, bottom: 8, left: 8 } },
    plugins,
    scales: {
      x: {
        type: 'logarithmic',
        min: xBounds.min,
        max: xBounds.max,
        grid: { color: chartTheme.grid },
        border: { color: chartTheme.axis },
        title: { display: true, text: labels.ttft, color: chartTheme.textSecondary, font: { size: 11, weight: 800 } },
        ticks: { color: chartTheme.textSecondary, font: { size: 10 }, maxTicksLimit: isMobile ? 4 : 6, callback: (value) => formatDurationMs(Number(value)) },
      },
      y: {
        type: 'logarithmic',
        min: yBounds.min,
        max: yBounds.max,
        grid: { color: chartTheme.grid },
        border: { color: chartTheme.axis },
        title: { display: true, text: labels.latency, color: chartTheme.textSecondary, font: { size: 11, weight: 800 } },
        ticks: { color: chartTheme.textSecondary, font: { size: 10 }, maxTicksLimit: isMobile ? 4 : 6, callback: (value) => formatDurationMs(Number(value)) },
      },
    },
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
  const chartData = useMemo(() => buildLatencyDiagnosticsChartData(safeDiagnostics, labels.samples, latencyColors), [safeDiagnostics, labels.samples, latencyColors]);
  const chartOptions = useMemo(() => buildLatencyDiagnosticsChartOptions({
    diagnostics: safeDiagnostics,
    chartTheme,
    isMobile,
    labels,
    colors: latencyColors,
  }), [chartTheme, isMobile, labels, latencyColors, safeDiagnostics]);
  const hasData = toNumber(safeDiagnostics.total_points) > 0 && safeDiagnostics.points.length > 0;
  return (
    <section className={`${styles.analysisCard} ${styles.latencyDiagnosticsCard}`}>
      <AnalysisCardHeader
        title={t('usage_stats.analysis_latency_title')}
        subtitle={t('usage_stats.analysis_latency_subtitle')}
        showPricingHint={false}
        hint=""
      />
      {loading ? (
        <div className={styles.emptyState}>{t('common.loading')}</div>
      ) : !hasData ? (
        <div className={styles.emptyState}>{t('usage_stats.no_data')}</div>
      ) : (
        <div className={styles.latencyDiagnosticsBody}>
          <div className={styles.latencyMetricGrid}>
            <div className={styles.latencyMetric}>
              <span>{t('usage_stats.analysis_latency_p95_ttft')}</span>
              <strong>{formatDurationMs(safeDiagnostics.p95_ttft_ms)}</strong>
            </div>
            <div className={styles.latencyMetric}>
              <span>{t('usage_stats.analysis_latency_p95_latency')}</span>
              <strong>{formatDurationMs(safeDiagnostics.p95_latency_ms)}</strong>
            </div>
            <div className={styles.latencyMetric}>
              <span>{t('usage_stats.analysis_latency_samples_count')}</span>
              <strong>{formatCompactNumber(safeDiagnostics.total_points)}</strong>
              {safeDiagnostics.sampled ? <small>{t('usage_stats.analysis_latency_sampled')}</small> : null}
            </div>
          </div>
          <div className={styles.analysisChartSurface}>
            <div className={styles.latencyChartFrame}>
              <Scatter data={chartData} options={chartOptions} plugins={[latencyDiagnosticsPlugin]} />
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

function CompositionPanel({ tabs, loading, isDark }: { tabs: CompositionTab[]; loading: boolean; isDark: boolean }) {
  const { t } = useTranslation();
  const [activeTabId, setActiveTabId] = useState<CompositionTab['id']>('api_key');
  const activeTab = tabs.find((tab) => tab.id === activeTabId) ?? tabs[0];
  const items = activeTab?.items ?? EMPTY_COMPOSITION_ITEMS;
  const activeContentKey = `${activeTab?.id ?? 'empty'}:${items.map((item) => item.key).join('|')}`;
  const chartTheme = useMemo(() => getChartTheme(isDark), [isDark]);
  const chartData = useMemo(() => buildCompositionChartData(items), [items]);
  const chartOptions = useMemo(() => buildCompositionChartOptions(chartTheme), [chartTheme]);
  const hasUnavailableCost = items.some((item) => item.cost_available === false);
  return (
    <section className={`${styles.analysisCard} ${styles.compositionCard}`}>
      <AnalysisCardHeader
        title={t('usage_stats.analysis_composition_title')}
        subtitle={t('usage_stats.analysis_composition_subtitle')}
        showPricingHint={hasUnavailableCost}
        hint={t('usage_stats.cost_need_price')}
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
                      <td>{formatUsd(toNumber(item.cost_usd))}</td>
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
  const blendedRate = getCostRatePerMillion(totalCost, totalTokens);
  const ratePoints: CostRatePoint[] = rows
    .filter((row) => row.total > 0)
    .map((row) => ({
      label: row.label,
      rate: getCostRatePerMillion(row.cost, row.total),
      cost: row.cost,
      tokens: row.total,
    }));
  const rateMax = Math.max(0, ...ratePoints.map((point) => point.rate));
  const segments: CostBreakdownSegment[] = [
    { key: 'input', label: t('usage_stats.input_tokens'), value: toNumber(safeBreakdown.input_cost_usd), color: TOKEN_COLORS.input.base, tokens: segmentTokens.input },
    { key: 'output', label: t('usage_stats.output_tokens'), value: toNumber(safeBreakdown.output_cost_usd), color: TOKEN_COLORS.output.base, tokens: segmentTokens.output },
    { key: 'cached', label: t('usage_stats.cached_tokens'), value: toNumber(safeBreakdown.cached_cost_usd), color: TOKEN_COLORS.cached.base, tokens: segmentTokens.cached },
  ];
  const hasData = rows.length > 0 || totalCost > 0 || segments.some((segment) => segment.value > 0);
  const buildCostTooltipLines = (segment: CostBreakdownSegment, percent: number) => [
    `${segment.label} · ${t('usage_stats.analysis_cost_share')}`,
    `${t('usage_stats.total_cost')}: ${formatUsd(segment.value)}`,
    `${t('usage_stats.analysis_cost_share')}: ${formatPercent(percent)}`,
    `${t('usage_stats.total_tokens')}: ${formatCompactNumber(segment.tokens)}`,
    `${t('usage_stats.analysis_cost_per_million_tokens')}: ${formatUsd(getCostRatePerMillion(segment.value, segment.tokens))}`,
  ];
  const buildRateTooltipLines = (point: CostRatePoint) => [
    point.label,
    `${t('usage_stats.analysis_cost_per_million_tokens')}: ${formatUsd(point.rate)}`,
    `${t('usage_stats.total_cost')}: ${formatUsd(point.cost)}`,
    `${t('usage_stats.total_tokens')}: ${formatCompactNumber(point.tokens)}`,
  ];
  const sparklineHint = t('usage_stats.analysis_cost_rate_sparkline_hint');
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
      <AnalysisCardHeader
        title={t('usage_stats.analysis_cost_breakdown_title')}
        subtitle={t('usage_stats.analysis_cost_breakdown_subtitle')}
        showPricingHint={!costAvailable}
        hint={t('usage_stats.cost_need_price')}
      />
      {loading ? (
        <div className={styles.emptyState}>{t('common.loading')}</div>
      ) : !hasData ? (
        <div className={styles.emptyState}>{t('usage_stats.no_data')}</div>
      ) : (
        <div className={styles.costBreakdownBody}>
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
              <strong>{formatUsd(totalCost)}</strong>
            </div>
            <div className={styles.costRateMetric}>
              <span>{t('usage_stats.analysis_cost_per_million_tokens')}</span>
              <strong>{formatUsd(blendedRate)}</strong>
              <small>{t('usage_stats.analysis_blended_rate')}</small>
            </div>
            <div className={styles.costRateSparkline} aria-label={sparklineHint} title={sparklineHint}>
              {ratePoints.length === 0 ? (
                <span className={styles.costRateSparkEmpty} />
              ) : ratePoints.slice(-12).map((point, index) => {
                const tooltipLines = buildRateTooltipLines(point);
                const tooltip = tooltipLines.join('\n');
                const ariaLabel = tooltipLines.join(', ');
                return (
                  <span
                    key={`${index}-${point.label}-${point.rate}`}
                    className={styles.costRateSparkBar}
                    style={{ height: `${Math.max(12, rateMax > 0 ? (point.rate / rateMax) * 100 : 0)}%` }}
                    title={tooltip}
                    aria-label={ariaLabel}
                    tabIndex={0}
                  />
                );
              })}
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
      <AnalysisCardHeader
        title={t('usage_stats.analysis_model_efficiency_title')}
        subtitle={t('usage_stats.analysis_model_efficiency_subtitle')}
        showPricingHint={hasUnavailableCost}
        hint={t('usage_stats.cost_need_price')}
      />
      {loading ? (
        <div className={styles.emptyState}>{t('common.loading')}</div>
      ) : !hasData ? (
        <div className={styles.emptyState}>{t('usage_stats.no_data')}</div>
      ) : (
        <div className={styles.modelEfficiencyBody}>
          {hasPricedData ? (
            <div className={styles.efficiencyChartFrame}>
              <Scatter data={chartData} options={chartOptions} />
            </div>
          ) : (
            <div className={styles.emptyState}>{t('usage_stats.no_data')}</div>
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
  const hasUnavailableCost = useMemo(() => cells.some((cell) => cell.cost_available === false), [cells]);
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
      `${t('usage_stats.total_cost')}: ${formatUsd(cost)}`,
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
      <AnalysisCardHeader
        title={t('usage_stats.analysis_heatmap_title')}
        subtitle={t('usage_stats.analysis_heatmap_subtitle')}
        showPricingHint={hasUnavailableCost}
        hint={t('usage_stats.cost_need_price')}
      />
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
  const tokenRows = useMemo(() => buildTokenUsageRows(analysis?.token_usage ?? [], analysis?.granularity ?? 'hourly', analysis?.timezone), [analysis]);
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
      <LatencyDiagnosticsCard diagnostics={analysis?.latency_diagnostics} loading={loading} isDark={isDark} isMobile={isMobile} />
      <CompositionPanel tabs={compositionTabs} loading={loading} isDark={isDark} />
      <Heatmap cells={analysis?.heatmap?.cells ?? []} apiKeys={analysis?.heatmap?.api_keys ?? []} apiKeyLabels={analysis?.heatmap?.api_key_labels ?? {}} models={analysis?.heatmap?.models ?? []} loading={loading} isDark={isDark} />
    </div>
  );
}
