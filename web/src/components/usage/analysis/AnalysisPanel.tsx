import { useMemo, type CSSProperties } from 'react';
import { useTranslation } from 'react-i18next';
import type { Chart, ChartData, ChartOptions, Plugin, TooltipModel } from 'chart.js';
import { Bar, Doughnut } from 'react-chartjs-2';
import type { AnalysisCompositionItem, AnalysisHeatmapCell, AnalysisResponse, AnalysisTokenUsageBucket } from '@/lib/types';
import { calculateDisplayInputTokens, calculateDisplayOutputTokens, formatCompactNumber } from '@/utils/usage';
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
};
const COMPOSITION_TOOLTIP_ID = 'analysis-composition-tooltip';
const COMPOSITION_TOOLTIP_MAX_WIDTH = 400;
const COMPOSITION_TOOLTIP_VIEWPORT_PADDING = 8;
type TokenLabels = {
  input: string;
  output: string;
  cached: string;
  reasoning: string;
  total: string;
  requests: string;
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

const getTooltipTokenValue = (dataset: unknown, dataIndex: number | undefined, fallback: unknown): number => {
  const tooltipData = (dataset as { tooltipData?: unknown[] }).tooltipData;
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

const getHeatmapCellGradient = (intensity: number) => {
  const clampedIntensity = Math.max(0, Math.min(1, intensity));
  const top = clampedIntensity <= 0.5
    ? interpolateColor([255, 250, 238], [226, 181, 98], clampedIntensity / 0.5)
    : interpolateColor([226, 181, 98], [214, 118, 96], (clampedIntensity - 0.5) / 0.5);
  const bottom = clampedIntensity <= 0.5
    ? interpolateColor([250, 244, 230], [214, 162, 76], clampedIntensity / 0.5)
    : interpolateColor([214, 162, 76], [198, 87, 70], (clampedIntensity - 0.5) / 0.5);
  return `linear-gradient(180deg, rgb(${top.join(', ')}) 0%, rgb(${bottom.join(', ')}) 100%)`;
};

const getHeatmapCellTextColor = (intensity: number) => {
  const clampedIntensity = Math.max(0, Math.min(1, intensity));
  const color = interpolateColor([107, 71, 35], [48, 24, 16], clampedIntensity);
  const opacity = 0.58 + clampedIntensity * 0.28;
  return `rgba(${color.join(', ')}, ${opacity})`;
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
  }));
}

function takeMajorComposition(items: AnalysisCompositionItem[], othersLabel: string, limit = 5): AnalysisCompositionItem[] {
  if (items.length <= limit) return items;
  const major = items.slice(0, limit);
  const rest = items.slice(limit).reduce(
    (sum, item) => ({
      total_tokens: sum.total_tokens + toNumber(item.total_tokens),
      requests: sum.requests + toNumber(item.requests),
    }),
    { total_tokens: 0, requests: 0 },
  );
  const total = items.reduce((sum, item) => sum + toNumber(item.total_tokens), 0);
  return [
    ...major,
    {
      key: '__others__',
      label: othersLabel,
      total_tokens: rest.total_tokens,
      requests: rest.requests,
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
            const label = context.dataset.label ? `${context.dataset.label}: ` : '';
            const value = getTooltipTokenValue(context.dataset, context.dataIndex, context.parsed.y);
            return `${label}${formatCompactNumber(value)}`;
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
    },
  };
}

function buildAnalysisTokenChartData(rows: ChartRow[], labels: TokenLabels): ChartData<'bar', number[], string> {
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
      } as unknown as ChartData<'bar', number[], string>['datasets'][number],
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

function getCompositionTooltipElement() {
  let tooltipEl = document.getElementById(COMPOSITION_TOOLTIP_ID) as HTMLDivElement | null;
  if (tooltipEl) return tooltipEl;
  tooltipEl = document.createElement('div');
  tooltipEl.id = COMPOSITION_TOOLTIP_ID;
  document.body.appendChild(tooltipEl);
  return tooltipEl;
}

function createCompositionTooltipHandler(chartTheme: ChartTheme): (args: { chart: Chart; tooltip: TooltipModel<'doughnut'> }) => void {
  return ({ chart, tooltip }) => {
    if (typeof document === 'undefined') return;
    const tooltipEl = getCompositionTooltipElement();
    if (tooltip.opacity === 0) {
      tooltipEl.style.opacity = '0';
      return;
    }

    tooltipEl.replaceChildren();
    const title = document.createElement('div');
    title.textContent = tooltip.title.join(' ');
    title.style.color = chartTheme.textPrimary;
    title.style.fontWeight = '800';
    title.style.marginBottom = '4px';
    tooltipEl.appendChild(title);

    for (const bodyItem of tooltip.body) {
      for (const line of bodyItem.lines) {
        const body = document.createElement('div');
        body.textContent = line;
        body.style.color = chartTheme.tooltipBody;
        tooltipEl.appendChild(body);
      }
    }

    const viewportWidth = window.innerWidth;
    const maxWidth = Math.min(COMPOSITION_TOOLTIP_MAX_WIDTH, viewportWidth - COMPOSITION_TOOLTIP_VIEWPORT_PADDING * 2);
    tooltipEl.style.position = 'fixed';
    tooltipEl.style.zIndex = '1000';
    tooltipEl.style.pointerEvents = 'none';
    tooltipEl.style.opacity = '1';
    tooltipEl.style.maxWidth = `${maxWidth}px`;
    tooltipEl.style.padding = '10px 12px';
    tooltipEl.style.border = `1px solid ${chartTheme.tooltipBorder}`;
    tooltipEl.style.borderRadius = '12px';
    tooltipEl.style.background = chartTheme.tooltipBg;
    tooltipEl.style.boxShadow = '0 16px 36px rgba(0, 0, 0, 0.18)';
    tooltipEl.style.font = '12px system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif';
    tooltipEl.style.lineHeight = '1.35';
    tooltipEl.style.whiteSpace = 'normal';
    tooltipEl.style.overflowWrap = 'anywhere';

    const canvasRect = chart.canvas.getBoundingClientRect();
    const tooltipWidth = tooltipEl.offsetWidth;
    const tooltipHeight = tooltipEl.offsetHeight;
    const rawLeft = canvasRect.left + tooltip.caretX - tooltipWidth / 2;
    const left = Math.max(COMPOSITION_TOOLTIP_VIEWPORT_PADDING, Math.min(rawLeft, viewportWidth - tooltipWidth - COMPOSITION_TOOLTIP_VIEWPORT_PADDING));
    const topAbove = canvasRect.top + tooltip.caretY - tooltipHeight - 12;
    const top = topAbove >= COMPOSITION_TOOLTIP_VIEWPORT_PADDING ? topAbove : canvasRect.top + tooltip.caretY + 12;
    tooltipEl.style.left = `${left}px`;
    tooltipEl.style.top = `${top}px`;
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
        enabled: false,
        external: createCompositionTooltipHandler(chartTheme),
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

function CompositionDonutChart({ title, items, loading, isDark }: { title: string; items: AnalysisCompositionItem[]; loading: boolean; isDark: boolean }) {
  const { t } = useTranslation();
  const chartTheme = useMemo(() => getChartTheme(isDark), [isDark]);
  const chartData = useMemo(() => buildCompositionChartData(items), [items]);
  const chartOptions = useMemo(() => buildCompositionChartOptions(chartTheme), [chartTheme]);
  return (
    <section className={styles.analysisCard}>
      <div className={styles.cardHeader}>
        <div>
          <h2>{title}</h2>
          <p>{t('usage_stats.analysis_composition_subtitle')}</p>
        </div>
      </div>
      {loading ? (
        <div className={styles.emptyState}>{t('common.loading')}</div>
      ) : items.length === 0 ? (
        <div className={styles.emptyState}>{t('usage_stats.no_data')}</div>
      ) : (
        <div className={styles.analysisChartSurface}>
          <div className={styles.donutLayout}>
            <div className={styles.donutChartFrame}>
              <Doughnut data={chartData} options={chartOptions} />
            </div>
            <div className={styles.compositionLegend}>
              {items.map((item, index) => (
                <div key={item.key} className={styles.compositionLegendRow}>
                  <span className={styles.legendDot} style={{ backgroundColor: CHART_COLORS[index % CHART_COLORS.length].base }} />
                  <span className={styles.legendName}>{item.label}</span>
                  <span className={styles.legendValue}>{formatPercent(item.percent)}</span>
                </div>
              ))}
            </div>
          </div>
        </div>
      )}
    </section>
  );
}

function Heatmap({ cells, apiKeys, models, loading }: { cells: AnalysisHeatmapCell[]; apiKeys: string[]; models: string[]; loading: boolean }) {
  const { t } = useTranslation();
  const cellMap = useMemo(() => new Map(cells.map((cell) => [`${cell.api_key}\0${cell.model}`, cell])), [cells]);
  const maxHeatmapTokens = useMemo(() => Math.max(0, ...cells.map((cell) => toNumber(cell.total_tokens))), [cells]);
  return (
    <section className={`${styles.analysisCard} ${styles.heatmapCard}`}>
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
              <div className={styles.heatmapGrid} style={{ gridTemplateColumns: `150px repeat(${models.length}, minmax(75px, 1fr))` }}>
                <div className={styles.heatmapCorner}>{t('usage_stats.analysis_heatmap_api_key')}</div>
                {models.map((model) => (
                  <div key={model} className={`${styles.heatmapHeaderCell} ${styles.heatmapTooltipTarget}`} data-full-name={model}>
                    <span className={styles.heatmapTruncatedLabel}>{model}</span>
                  </div>
                ))}
                {apiKeys.map((apiKey) => (
                  <div key={apiKey} className={styles.heatmapRowContents}>
                    <div className={`${styles.heatmapRowLabel} ${styles.heatmapTooltipTarget}`} data-full-name={apiKey}>
                      <span className={styles.heatmapTruncatedLabel}>{apiKey}</span>
                    </div>
                    {models.map((model) => {
                      const cell = cellMap.get(`${apiKey}\0${model}`);
                      const heatmapTokens = toNumber(cell?.total_tokens);
                      const heatmapRequests = toNumber(cell?.requests);
                      const intensity = getHeatmapVisualIntensity(heatmapTokens, maxHeatmapTokens);
                      return (
                        <div
                          key={`${apiKey}-${model}`}
                          className={styles.heatmapCell}
                          style={{ background: getHeatmapCellGradient(intensity), color: getHeatmapCellTextColor(intensity) } as CSSProperties}
                          title={t('usage_stats.analysis_heatmap_cell_title', {
                            model,
                            tokens: formatCompactNumber(heatmapTokens),
                            requests: formatCompactNumber(heatmapRequests),
                          })}
                        >
                          <span className={styles.heatmapCellTokenValue}>
                            {t('usage_stats.analysis_heatmap_tokens_prefix')}: {formatCompactNumber(heatmapTokens)}
                          </span>
                          <span className={styles.heatmapCellRequestValue}>
                            {t('usage_stats.analysis_heatmap_requests_prefix')}: {formatCompactNumber(heatmapRequests)}
                          </span>
                        </div>
                      );
                    })}
                  </div>
                ))}
              </div>
            </div>
          </div>
          <div className={styles.heatmapLegend} aria-label={t('usage_stats.analysis_heatmap_legend')}>
            <span>{t('usage_stats.analysis_heatmap_low')}</span>
            <span className={styles.heatmapLegendRamp} aria-hidden="true" />
            <span>{t('usage_stats.analysis_heatmap_high')}</span>
          </div>
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

  return (
    <div className={styles.analysisPanel}>
      <TokenUsageChart rows={tokenRows} loading={loading} isDark={isDark} isMobile={isMobile} />
      <div className={styles.compositionGrid}>
        <CompositionDonutChart title={t('usage_stats.analysis_api_key_composition_title')} items={apiComposition} loading={loading} isDark={isDark} />
        <CompositionDonutChart title={t('usage_stats.analysis_model_composition_title')} items={modelComposition} loading={loading} isDark={isDark} />
        <CompositionDonutChart title={t('usage_stats.analysis_auth_files_composition_title')} items={authFilesComposition} loading={loading} isDark={isDark} />
        <CompositionDonutChart title={t('usage_stats.analysis_ai_provider_composition_title')} items={aiProviderComposition} loading={loading} isDark={isDark} />
      </div>
      <Heatmap cells={analysis?.heatmap.cells ?? []} apiKeys={analysis?.heatmap.api_keys ?? []} models={analysis?.heatmap.models ?? []} loading={loading} />
    </div>
  );
}
