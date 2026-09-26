import { useMemo } from 'react';
import '@/lib/chartjs';
import type { Chart, ChartData, ChartOptions, Plugin } from 'chart.js';
import { Scatter } from 'react-chartjs-2';
import { formatDurationMs } from '@/utils/usage';
import { getUsageChartTheme, type UsageChartTheme } from '@/utils/usage/chartConfig';

export interface LatencyScatterData {
  points: Array<{ ttft_ms: number; latency_ms: number }>;
  total_points: number;
  p95_ttft_ms: number;
  p95_latency_ms: number;
  max_ttft_ms: number;
  max_latency_ms: number;
}

export interface LatencyScatterLabels {
  ttft: string;
  latency: string;
  p95TTFT: string;
  p95Latency: string;
  samples: string;
}

type ChartTheme = UsageChartTheme;
const toNumber = (value: unknown) => {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : 0;
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

const LATENCY_COLORS = {
  light: {
    pointFill: 'rgba(45, 212, 191, 0.62)',
    p95TTFT: '#38bdf8',
    p95Latency: '#fb7185',
  },
  dark: {
    pointFill: 'rgba(94, 234, 212, 0.72)',
    p95TTFT: '#7dd3fc',
    p95Latency: '#fda4af',
  },
} satisfies Record<'light' | 'dark', LatencyThemeColors>;
const LATENCY_REFERENCE_HIT_RADIUS_PX = 8;
const latencyReferenceHoverStates = new WeakMap<Chart<'scatter'>, LatencyReferenceHover>();

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
  formatDuration: (value: number) => string,
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
        text: `${options.labels.p95TTFT}: ${formatDuration(options.p95TTFTMS)}`,
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
        text: `${options.labels.p95Latency}: ${formatDuration(options.p95LatencyMS)}`,
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

const createLatencyDiagnosticsPlugin = (formatDuration: (value: number) => string): Plugin<'scatter'> => ({
  id: 'analysis-latency-diagnostics',
  afterEvent: (chart, args) => {
    const options = getLatencyDiagnosticsPluginOptions(chart);
    if (!options) return;
    if (args.event.type === 'mouseout' || !args.inChartArea) {
      setLatencyReferenceHover(chart, args, undefined);
      return;
    }
    if (args.event.type !== 'mousemove') return;
    setLatencyReferenceHover(chart, args, getLatencyReferenceHover(chart, args.event, options, formatDuration));
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

function* getLatencyAxisValues(diagnostics: LatencyScatterData, axis: 'ttft' | 'latency'): Generator<number> {
  yield axis === 'ttft' ? diagnostics.max_ttft_ms : diagnostics.max_latency_ms;
  yield axis === 'ttft' ? diagnostics.p95_ttft_ms : diagnostics.p95_latency_ms;
  for (const point of diagnostics.points) {
    yield axis === 'ttft' ? point.ttft_ms : point.latency_ms;
  }
}

function buildLatencyDiagnosticsChartData(diagnostics: LatencyScatterData, label: string, colors: LatencyThemeColors): ChartData<'scatter', LatencyScatterPoint[], string> {
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
  formatDuration,
}: {
  diagnostics: LatencyScatterData;
  chartTheme: ChartTheme;
  isMobile: boolean;
  labels: LatencyScatterLabels;
  colors: LatencyThemeColors;
  formatDuration: (value: number) => string;
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
          `${labels.ttft}: ${formatDuration(Number(context.parsed.x))}`,
          `${labels.latency}: ${formatDuration(Number(context.parsed.y))}`,
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
        ticks: { color: chartTheme.textSecondary, font: { size: 10 }, maxTicksLimit: isMobile ? 4 : 6, callback: (value) => formatDuration(Number(value)) },
      },
      y: {
        type: 'logarithmic',
        min: yBounds.min,
        max: yBounds.max,
        grid: { color: chartTheme.grid },
        border: { color: chartTheme.axis },
        title: { display: true, text: labels.latency, color: chartTheme.textSecondary, font: { size: 11, weight: 800 } },
        ticks: { color: chartTheme.textSecondary, font: { size: 10 }, maxTicksLimit: isMobile ? 4 : 6, callback: (value) => formatDuration(Number(value)) },
      },
    },
  };
}

export function LatencyScatterChart({ diagnostics, isDark, isMobile, labels, formatDuration = formatDurationMs }: {
  diagnostics: LatencyScatterData;
  isDark: boolean;
  isMobile: boolean;
  labels: LatencyScatterLabels;
  formatDuration?: (value: number) => string;
}) {
  const chartTheme = useMemo(() => getUsageChartTheme(isDark), [isDark]);
  const colors = useMemo(() => getLatencyColors(isDark), [isDark]);
  const chartData = useMemo(() => buildLatencyDiagnosticsChartData(diagnostics, labels.samples, colors), [diagnostics, labels.samples, colors]);
  const chartOptions = useMemo(() => buildLatencyDiagnosticsChartOptions({ diagnostics, chartTheme, isMobile, labels, colors, formatDuration }), [diagnostics, chartTheme, isMobile, labels, colors, formatDuration]);
  const plugin = useMemo(() => createLatencyDiagnosticsPlugin(formatDuration), [formatDuration]);
  return <Scatter data={chartData} options={chartOptions} plugins={[plugin]} />;
}
