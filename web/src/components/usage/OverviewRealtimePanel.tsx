import { RealtimeCacheChart, RealtimeDiagnostics, RealtimeWindowCards } from './RealtimeInsights';
import { RealtimeTokenShareRibbons } from './RealtimeTokenShareRibbons';
import { useMemo, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import '@/lib/chartjs';
import type { ChartData, ChartOptions, Plugin } from 'chart.js';
import { Line } from 'react-chartjs-2';
import { LatencyScatterChart } from './LatencyScatterChart';
import type {
  OverviewRealtimeBlock,
  OverviewRealtimeWindow,
  RealtimeUsageTopItem,
} from '@/lib/types';
import {
  formatCompactNumber,
  formatDurationMs,
  formatFixedTwoDecimals,
  formatPerMinuteValue,
} from '@/utils/usage';
import { LoadingSpinner } from '@/components/ui/LoadingSpinner';
import styles from '@/pages/UsagePage.module.scss';

type RealtimeDimensionKey = 'models' | 'api_keys' | 'auth_files' | 'ai_providers';

interface RealtimeDimension {
  key: RealtimeDimensionKey;
  labelKey: string;
  items: RealtimeUsageTopItem[];
}

interface RealtimeMetric {
  label: string;
  value: string;
  tone?: 'up' | 'down' | 'flat';
}

interface RealtimeMetricPair {
  label: string;
  tokenValue: string;
  requestValue: string;
  tokenTone?: RealtimeMetric['tone'];
  requestTone?: RealtimeMetric['tone'];
}

interface RealtimeThroughputPoint {
  bucket: string;
  tokensPerMinute: number | null;
  requestsPerMinute: number | null;
}


interface OverviewRealtimePanelProps {
  realtime?: OverviewRealtimeBlock;
  loading: boolean;
  error?: string;
  window: OverviewRealtimeWindow;
  onWindowChange: (window: OverviewRealtimeWindow) => void;
  isDark: boolean;
  isMobile: boolean;
  timezone?: string;
  visibleDimensions?: readonly RealtimeDimensionKey[];
}

const REALTIME_WINDOWS: OverviewRealtimeWindow[] = ['15m', '30m', '60m'];
const DEFAULT_VISIBLE_DIMENSIONS: readonly RealtimeDimensionKey[] = ['models', 'api_keys', 'auth_files', 'ai_providers'];
const THROUGHPUT_Y_TICK_COUNT = 6;
const THROUGHPUT_Y_INTERVAL_COUNT = THROUGHPUT_Y_TICK_COUNT - 1;
const THROUGHPUT_LEGEND_BOTTOM_GAP = 10;

const CHART_COLORS = {
  token: '#3b82f6',
  request: '#f97316',
} as const;

const REALTIME_DURATION_UNITS = { d: 'd', h: 'h', m: 'm', s: 's', ms: 'ms' } as const;
const formatRealtimeDuration = (value: number) => formatDurationMs(value, {
  maxUnits: 2,
  locale: 'en-US',
  unitLabels: REALTIME_DURATION_UNITS,
});

const throughputLegendSpacingPlugin: Plugin<'line'> = {
  id: 'throughputLegendSpacing',
  afterInit: (chart) => {
    const legend = chart.legend;
    if (!legend) return;
    const fit = legend.fit.bind(legend);
    // 图例保持在顶部原位，只把绘图区下移，留出清晰的纵向呼吸空间。
    legend.fit = () => {
      fit();
      legend.height += THROUGHPUT_LEGEND_BOTTOM_GAP;
    };
  },
};

const THROUGHPUT_CHART_PLUGINS: Plugin<'line'>[] = [throughputLegendSpacingPlugin];

const emptyRealtime = (window: OverviewRealtimeWindow): OverviewRealtimeBlock => ({
  window,
  bucket_seconds: window === '30m' ? 60 : window === '60m' ? 120 : 30,
  token_velocity: [],
  latency_scatter: { points: [], total_points: 0, p95_ttft_ms: 0, p95_latency_ms: 0, max_ttft_ms: 0, max_latency_ms: 0 },
  current_usage: {
    models: [],
    api_keys: [],
    auth_files: [],
    ai_providers: [],
  },
  request_level: [],
  cache_level: [],
});

const getIntlTimeZone = (timezone: string | undefined) => {
  const trimmed = timezone?.trim();
  if (!trimmed || trimmed === 'Local') return undefined;
  return trimmed;
};

const formatBucketLabelFromLiteral = (bucket: string): string | null => {
  const match = bucket.match(/^\d{4}-\d{2}-\d{2}[T\s](\d{2}):(\d{2})(?::(\d{2}))?/);
  if (!match) return null;
  const hour = Number(match[1]);
  const minute = Number(match[2]);
  const second = match[3] ? Number(match[3]) : 0;
  if (hour < 0 || hour > 23 || minute < 0 || minute > 59 || second < 0 || second > 59) return null;
  const label = `${String(hour).padStart(2, '0')}:${String(minute).padStart(2, '0')}`;
  return second === 0 ? label : `${label}:${String(second).padStart(2, '0')}`;
};

const formatBucketLabel = (bucket: string, timezone?: string): string => {
  const parsed = Date.parse(bucket);
  if (!Number.isFinite(parsed)) return bucket;
  const date = new Date(parsed);
  const timeZone = getIntlTimeZone(timezone);
  try {
    const parts = new Intl.DateTimeFormat('en-GB', {
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
      hourCycle: 'h23',
      timeZone,
    }).formatToParts(date);
    const hour = parts.find((part) => part.type === 'hour')?.value ?? '00';
    const minute = parts.find((part) => part.type === 'minute')?.value ?? '00';
    const second = parts.find((part) => part.type === 'second')?.value ?? '00';
    return second === '00' ? `${hour}:${minute}` : `${hour}:${minute}:${second}`;
  } catch {
    const literalLabel = formatBucketLabelFromLiteral(bucket);
    if (literalLabel) return literalLabel;
  }
  const h = date.getHours().toString().padStart(2, '0');
  const m = date.getMinutes().toString().padStart(2, '0');
  const s = date.getSeconds().toString().padStart(2, '0');
  return s === '00' ? `${h}:${m}` : `${h}:${m}:${s}`;
};

const safeNumber = (value: unknown): number => {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : 0;
};

function buildRealtimeThroughputPoints(data: OverviewRealtimeBlock): RealtimeThroughputPoint[] {
  const points = new Map<string, RealtimeThroughputPoint>();
  // 两组 API 序列都按 bucket 合并，避免单侧缺点时把不同时间的值画在一起。
  data.token_velocity.forEach((point) => {
    points.set(point.bucket, {
      bucket: point.bucket,
      tokensPerMinute: safeNumber(point.tokens_per_minute),
      requestsPerMinute: null,
    });
  });
  data.request_level.forEach((point) => {
    const current = points.get(point.bucket);
    points.set(point.bucket, {
      bucket: point.bucket,
      tokensPerMinute: current?.tokensPerMinute ?? null,
      requestsPerMinute: safeNumber(point.requests_per_minute),
    });
  });
  return Array.from(points.values()).sort((left, right) => {
    const leftTime = Date.parse(left.bucket);
    const rightTime = Date.parse(right.bucket);
    if (Number.isFinite(leftTime) && Number.isFinite(rightTime)) return leftTime - rightTime;
    return left.bucket.localeCompare(right.bucket);
  });
}

function throughputRequestAxisMax(values: Array<number | null>): number {
  const maximum = values.reduce<number>((result, value) => (
    typeof value === 'number' && Number.isFinite(value) ? Math.max(result, value) : result
  ), 0);
  const integerStep = Math.max(1, Math.ceil(maximum / THROUGHPUT_Y_INTERVAL_COUNT));
  return integerStep * THROUGHPUT_Y_INTERVAL_COUNT;
}

const latestNumber = (values: Array<number | null>): number | null => {
  for (let index = values.length - 1; index >= 0; index -= 1) {
    const value = values[index];
    if (typeof value === 'number' && Number.isFinite(value)) {
      return value;
    }
  }
  return null;
};

const averageNumber = (values: Array<number | null>): number | null => {
  const finiteValues = values.filter((value): value is number => typeof value === 'number' && Number.isFinite(value));
  if (finiteValues.length === 0) return null;
  return finiteValues.reduce((sum, value) => sum + value, 0) / finiteValues.length;
};

const hasFiniteNumber = (values: Array<number | null>): boolean => values.some((value) => typeof value === 'number' && Number.isFinite(value));

const trendMetric = (
  values: Array<number | null>,
  formatter: (value: number) => string,
  label: string,
  options: { invertTone?: boolean; prefix?: string } = {},
): RealtimeMetric => {
  const half = Math.max(1, Math.floor(values.length / 2));
  const previous = averageNumber(values.slice(0, half));
  const recent = averageNumber(values.slice(half));
  if (previous === null || recent === null || previous <= 0) {
    return { label: options.prefix ? `${options.prefix} ${label}` : label, value: '--', tone: 'flat' };
  }
  const delta = ((recent - previous) / previous) * 100;
  const toneIsUp = options.invertTone ? delta < 0 : delta > 0;
  return {
    label: options.prefix ? `${options.prefix} ${label}` : label,
    value: `${delta >= 0 ? '+' : ''}${formatFixedTwoDecimals(delta)}%`,
    tone: Math.abs(delta) < 0.01 ? 'flat' : toneIsUp ? 'up' : 'down',
  };
};

const metricChips = (
  values: Array<number | null>,
  formatter: (value: number) => string,
  averageLabel: string,
  latestLabel: string,
  trendLabel: string,
  options: { invertTone?: boolean; prefix?: string } = {},
): RealtimeMetric[] => {
  const latest = latestNumber(values);
  const average = averageNumber(values);
  const prefix = options.prefix ? `${options.prefix} ` : '';
  return [
    { label: `${prefix}${latestLabel}`, value: latest === null ? '--' : formatter(latest) },
    { label: `${prefix}${averageLabel}`, value: average === null ? '--' : formatter(average) },
    trendMetric(values, formatter, trendLabel, options),
  ];
};

function buildThroughputOptions(
  isDark: boolean,
  isMobile: boolean,
  tokenLabel: string,
  requestLabel: string,
  requestValues: Array<number | null>,
): ChartOptions<'line'> {
  const gridColor = isDark ? 'rgba(255, 255, 255, 0.07)' : 'rgba(17, 24, 39, 0.07)';
  const tickColor = isDark ? 'rgba(255, 255, 255, 0.66)' : 'rgba(17, 24, 39, 0.66)';
  const tooltipBg = isDark ? 'rgba(17, 24, 39, 0.94)' : 'rgba(255, 255, 255, 0.98)';
  const tooltipText = isDark ? '#ffffff' : '#111827';
  const requestMax = throughputRequestAxisMax(requestValues);
  return {
    responsive: true,
    maintainAspectRatio: false,
    interaction: { mode: 'index', intersect: false },
    plugins: {
      legend: {
        display: true,
        position: 'top',
        align: 'end',
        labels: {
          color: tickColor,
          boxHeight: 2,
          usePointStyle: true,
          pointStyle: 'line',
          pointStyleWidth: isMobile ? 24 : 32,
          padding: isMobile ? 10 : 16,
          font: { size: isMobile ? 10 : 11 },
          generateLabels: (chart) => chart.data.datasets.map((dataset, datasetIndex) => {
            const requestSeries = datasetIndex === 1;
            return {
              text: dataset.label ?? '',
              fillStyle: 'transparent',
              fontColor: tickColor,
              hidden: !chart.isDatasetVisible(datasetIndex),
              lineCap: 'butt',
              lineDash: requestSeries ? [6, 4] : [],
              lineDashOffset: 0,
              lineJoin: 'miter',
              lineWidth: isMobile ? 1.6 : 2,
              strokeStyle: requestSeries ? CHART_COLORS.request : CHART_COLORS.token,
              pointStyle: 'line',
              rotation: 0,
              datasetIndex,
            };
          }),
        },
      },
      tooltip: {
        backgroundColor: tooltipBg,
        titleColor: tooltipText,
        bodyColor: tooltipText,
        borderColor: isDark ? 'rgba(255, 255, 255, 0.10)' : 'rgba(17, 24, 39, 0.10)',
        borderWidth: 1,
        padding: 10,
        displayColors: true,
        callbacks: {
          label: (context) => {
            const label = context.dataset.label ? `${context.dataset.label}: ` : '';
            const value = Number(context.parsed.y ?? 0);
            const formatted = context.dataset.yAxisID === 'requests'
              ? formatPerMinuteValue(value)
              : formatCompactNumber(value);
            return `${label}${formatted}`;
          },
        },
      },
    },
    scales: {
      x: {
        grid: { display: false },
        border: { color: gridColor },
        ticks: {
          color: tickColor,
          maxTicksLimit: isMobile ? 5 : 8,
          font: { size: isMobile ? 10 : 11 },
        },
      },
      tokens: {
        type: 'linear',
        position: 'left',
        beginAtZero: true,
        grid: { color: gridColor },
        border: { color: gridColor },
        title: {
          display: true,
          text: tokenLabel,
          color: tickColor,
          font: { size: isMobile ? 10 : 11 },
        },
        ticks: {
          color: tickColor,
          font: { size: isMobile ? 10 : 11 },
          count: THROUGHPUT_Y_TICK_COUNT,
          callback: (value) => formatCompactNumber(Number(value)),
        },
      },
      requests: {
        type: 'linear',
        position: 'right',
        beginAtZero: true,
        max: requestMax,
        grid: { drawOnChartArea: false },
        border: { color: gridColor },
        title: {
          display: true,
          text: requestLabel,
          color: tickColor,
          font: { size: isMobile ? 10 : 11 },
        },
        ticks: {
          color: tickColor,
          font: { size: isMobile ? 10 : 11 },
          count: THROUGHPUT_Y_TICK_COUNT,
          precision: 0,
          callback: (value) => Math.round(Number(value)).toLocaleString(),
        },
      },
    },
    elements: {
      line: { tension: 0.35, borderWidth: isMobile ? 1.6 : 2 },
      point: { radius: 0, hoverRadius: 3 },
    },
  };
}

function buildThroughputData(
  labels: string[],
  tokenLabel: string,
  requestLabel: string,
  tokenValues: Array<number | null>,
  requestValues: Array<number | null>,
): ChartData<'line', Array<number | null>, string> {
  return {
    labels,
    datasets: [
      {
        label: tokenLabel,
        data: tokenValues,
        yAxisID: 'tokens',
        borderColor: CHART_COLORS.token,
        backgroundColor: `${CHART_COLORS.token}24`,
        fill: true,
      },
      {
        label: requestLabel,
        data: requestValues,
        yAxisID: 'requests',
        borderColor: CHART_COLORS.request,
        backgroundColor: CHART_COLORS.request,
        borderDash: [6, 4],
        fill: false,
      },
    ],
  };
}

function RealtimeMetricPills({ metrics, metricsTooltip, labelPrefix }: { metrics: RealtimeMetric[]; metricsTooltip?: string; labelPrefix?: string }) {
  return (
    <div className={styles.overviewRealtimeMetrics}>
      {metrics.map((metric) => (
        <span
          key={metric.label}
          className={`${styles.overviewRealtimeMetric} ${metric.tone === 'up' ? styles.overviewRealtimeMetricUp : metric.tone === 'down' ? styles.overviewRealtimeMetricDown : metric.tone === 'flat' ? styles.overviewRealtimeMetricFlat : ''}`.trim()}
          title={metricsTooltip}
          aria-label={metricsTooltip ? `${labelPrefix ? `${labelPrefix} ` : ''}${metric.label} ${metric.value} ${metricsTooltip}` : undefined}
        >
          <span className={styles.overviewRealtimeMetricLabel}>{metric.label}</span>
          <span className={styles.overviewRealtimeMetricValue}>{metric.value}</span>
        </span>
      ))}
    </div>
  );
}

function RealtimePairedMetrics({
  metrics,
  tokenLabel,
  requestLabel,
  tokenShortLabel,
  requestShortLabel,
  metricsTooltip,
}: {
  metrics: RealtimeMetricPair[];
  tokenLabel: string;
  requestLabel: string;
  tokenShortLabel: string;
  requestShortLabel: string;
  metricsTooltip?: string;
}) {
  const toneClass = (tone: RealtimeMetric['tone']) => (
    tone === 'up'
      ? styles.overviewRealtimePairedMetricValueUp
      : tone === 'down'
        ? styles.overviewRealtimePairedMetricValueDown
        : tone === 'flat'
          ? styles.overviewRealtimePairedMetricValueFlat
          : ''
  );
  return (
    <div className={styles.overviewRealtimePairedMetrics}>
      {metrics.map((metric) => (
        <span
          key={metric.label}
          className={styles.overviewRealtimePairedMetric}
          title={metricsTooltip}
        >
          <span className={styles.overviewRealtimeScreenReaderOnly}>
            {`${metric.label} ${tokenLabel} ${metric.tokenValue} ${requestLabel} ${metric.requestValue}${metricsTooltip ? ` ${metricsTooltip}` : ''}`}
          </span>
          <span className={styles.overviewRealtimePairedMetricLabel} aria-hidden="true">{metric.label}</span>
          <span className={styles.overviewRealtimePairedMetricSeries} aria-hidden="true">
            <span className={styles.overviewRealtimePairedMetricTokenLabel}>{tokenShortLabel}</span>
            <span className={`${styles.overviewRealtimePairedMetricValue} ${toneClass(metric.tokenTone)}`.trim()}>{metric.tokenValue}</span>
          </span>
          <span className={styles.overviewRealtimePairedMetricDivider} aria-hidden="true">·</span>
          <span className={styles.overviewRealtimePairedMetricSeries} aria-hidden="true">
            <span className={styles.overviewRealtimePairedMetricRequestLabel}>{requestShortLabel}</span>
            <span className={`${styles.overviewRealtimePairedMetricValue} ${toneClass(metric.requestTone)}`.trim()}>{metric.requestValue}</span>
          </span>
        </span>
      ))}
    </div>
  );
}

function RealtimeCard({
  title,
  metrics,
  headerContent,
  children,
  full = false,
  className,
  metricsTooltip,
}: {
  title: string;
  metrics?: RealtimeMetric[];
  headerContent?: ReactNode;
  children: ReactNode;
  full?: boolean;
  className?: string;
  metricsTooltip?: string;
}) {
  const cardClassName = [
    styles.overviewRealtimeCard,
    'keeper-card-surface',
    full ? styles.overviewRealtimeCardFull : '',
    className ?? '',
  ].filter(Boolean).join(' ');
  return (
    <section className={cardClassName}>
      <div className={styles.overviewRealtimeCardHeader}>
        <div className="keeper-card-title-track">
          <h3 className="keeper-card-title">{title}</h3>
        </div>
        {headerContent ?? (metrics && metrics.length > 0 ? (
          <RealtimeMetricPills metrics={metrics} metricsTooltip={metricsTooltip} />
        ) : null)}
      </div>
      {children}
    </section>
  );
}

function RealtimeChartFrame({ loading, emptyLabel, children }: { loading: boolean; emptyLabel?: string; children: ReactNode }) {
  return (
    <div className={styles.overviewRealtimeChartFrame} aria-busy={loading}>
      {children}
      {emptyLabel && (
        <div className={styles.overviewRealtimeEmptyOverlay} role="status">
          <span>{emptyLabel}</span>
        </div>
      )}
    </div>
  );
}

export function OverviewRealtimePanel({ realtime, loading, error, window, onWindowChange, isDark, isMobile, timezone, visibleDimensions = DEFAULT_VISIBLE_DIMENSIONS }: OverviewRealtimePanelProps) {
  const { t } = useTranslation();
  const data = realtime ?? emptyRealtime(window);
  const initialLoading = loading && !realtime;
  const hasRealtimeData = realtime !== undefined && realtime !== null;
  const showInlineError = Boolean(error && hasRealtimeData);
  const showErrorOnly = Boolean(error && !hasRealtimeData);
  const [activeDimension, setActiveDimension] = useState<RealtimeDimensionKey>('models');
  const throughputPoints = useMemo(() => buildRealtimeThroughputPoints(data), [data]);
  const labels = useMemo(() => throughputPoints.map((point) => formatBucketLabel(point.bucket, data.timezone ?? timezone)), [data.timezone, throughputPoints, timezone]);
  const cacheLabels = useMemo(() => data.cache_level.map((point) => formatBucketLabel(point.bucket, data.timezone ?? timezone)), [data.cache_level, data.timezone, timezone]);

  const tokenValues = useMemo(() => throughputPoints.map((point) => point.tokensPerMinute), [throughputPoints]);
  const requestValues = useMemo(() => throughputPoints.map((point) => point.requestsPerMinute), [throughputPoints]);
  const cacheValues = useMemo(() => data.cache_level.map((point) => point.cache_read_rate == null ? null : safeNumber(point.cache_read_rate)), [data.cache_level]);
  const outcomeLabels = useMemo(() => (data.insights?.outcomes ?? []).map(point => formatBucketLabel(point.bucket, data.timezone ?? timezone)), [data.insights?.outcomes, data.timezone, timezone]);
  const latencyScatter = data.latency_scatter ?? { points: [], total_points: 0, p95_ttft_ms: 0, p95_latency_ms: 0, max_ttft_ms: 0, max_latency_ms: 0 };
  const throughputEmptyLabel = throughputPoints.length === 0 ? t('usage_stats.overview_realtime_throughput_empty') : undefined;
  const latencyEmptyLabel = latencyScatter.total_points === 0 ? t('usage_stats.no_data') : undefined;
  const cacheEmptyLabel = !hasFiniteNumber(cacheValues) && !data.cache_level.some(point => point.input_tokens > 0 || point.cache_read_tokens > 0 || point.cache_creation_tokens > 0) ? t('usage_stats.overview_realtime_cache_empty') : undefined;

  const latestLabel = t('usage_stats.overview_realtime_latest');
  const averageLabel = t('usage_stats.overview_realtime_average');
  const trendLabel = t('usage_stats.overview_realtime_trend');
  const rollingMetricHint = t('usage_stats.overview_realtime_rolling_metric_hint');
  const throughputMetricHint = t('usage_stats.overview_realtime_throughput_hint');
  const tokenRateLabel = t('usage_stats.overview_realtime_tpm');
  const requestRateLabel = t('usage_stats.overview_realtime_rpm');
  const tokenShortLabel = t('usage_stats.tpm');
  const requestShortLabel = t('usage_stats.rpm');

  const throughputOptions = useMemo(() => buildThroughputOptions(isDark, isMobile, tokenRateLabel, requestRateLabel, requestValues), [isDark, isMobile, requestRateLabel, requestValues, tokenRateLabel]);
  const throughputChartData = useMemo(() => buildThroughputData(labels, tokenRateLabel, requestRateLabel, tokenValues, requestValues), [labels, requestRateLabel, requestValues, tokenRateLabel, tokenValues]);
  const latencyLabels = useMemo(() => ({
    ttft: t('usage_stats.ttft'),
    latency: t('usage_stats.latency'),
    p95TTFT: t('usage_stats.analysis_latency_p95_ttft'),
    p95Latency: t('usage_stats.analysis_latency_p95_latency'),
    samples: t('usage_stats.analysis_latency_samples'),
  }), [t]);
  const latencyMetrics = [
    { label: latencyLabels.p95TTFT, value: latencyScatter.total_points ? formatRealtimeDuration(latencyScatter.p95_ttft_ms) : '--' },
    { label: latencyLabels.p95Latency, value: latencyScatter.total_points ? formatRealtimeDuration(latencyScatter.p95_latency_ms) : '--' },
    { label: t('usage_stats.analysis_latency_samples_count'), value: formatCompactNumber(latencyScatter.total_points) },
  ];
  const throughputMetrics = useMemo<RealtimeMetricPair[]>(() => {
    const tokenMetrics = metricChips(tokenValues, formatCompactNumber, averageLabel, latestLabel, trendLabel);
    const requestMetrics = metricChips(requestValues, formatPerMinuteValue, averageLabel, latestLabel, trendLabel);
    return tokenMetrics.map((tokenMetric, index) => ({
      label: tokenMetric.label,
      tokenValue: tokenMetric.value,
      requestValue: requestMetrics[index]?.value ?? '--',
      tokenTone: tokenMetric.tone,
      requestTone: requestMetrics[index]?.tone,
    }));
  }, [averageLabel, latestLabel, requestValues, tokenValues, trendLabel]);

  const dimensions = useMemo<RealtimeDimension[]>(() => {
    const next: RealtimeDimension[] = [
      { key: 'models', labelKey: 'usage_stats.overview_realtime_dimension_models', items: data.current_usage.models },
      { key: 'api_keys', labelKey: 'usage_stats.overview_realtime_dimension_api_keys', items: data.current_usage.api_keys },
      { key: 'auth_files', labelKey: 'usage_stats.overview_realtime_dimension_auth_files', items: data.current_usage.auth_files },
      { key: 'ai_providers', labelKey: 'usage_stats.overview_realtime_dimension_ai_providers', items: data.current_usage.ai_providers },
    ];
    const visible = new Set(visibleDimensions);
    return next.filter((dimension) => visible.has(dimension.key));
  }, [data.current_usage.ai_providers, data.current_usage.api_keys, data.current_usage.auth_files, data.current_usage.models, visibleDimensions]);
  const visibleDimension = dimensions.find((dimension) => dimension.key === activeDimension) ?? dimensions[0];

  return (
    <div className={styles.overviewRealtimeSection}>
      <div className={styles.overviewRealtimeToolbar}>
        <div className={styles.overviewRealtimeHeading}>
          <h2 className={styles.overviewRealtimeTitle}>{t('usage_stats.overview_realtime_section_title')}</h2>
        </div>
        <div className={styles.overviewRealtimeWindowSwitcher} role="group" aria-label={t('usage_stats.overview_realtime_window')}>
          {REALTIME_WINDOWS.map((option) => (
            <button
              key={option}
              type="button"
              className={`${styles.overviewRealtimeWindowButton} ${window === option ? styles.overviewRealtimeWindowButtonActive : ''}`.trim()}
              onClick={() => onWindowChange(option)}
              aria-pressed={window === option}
            >
              {option}
            </button>
          ))}
        </div>
      </div>

      {showErrorOnly ? (
        <div className={styles.errorBox}>{error}</div>
      ) : initialLoading ? (
        <div className={styles.overviewRealtimeLoading} aria-busy="true">
          <LoadingSpinner size={18} />
          <span>{t('common.loading')}</span>
        </div>
      ) : (
        <>
          {showInlineError && <div className={styles.errorBox}>{error}</div>}
          {data.insights && <RealtimeWindowCards summary={data.insights.summary} window={data.window} />}
          <div className={styles.overviewRealtimeGrid}>
          <RealtimeCard
            title={t('usage_stats.overview_realtime_throughput')}
            headerContent={(
              <RealtimePairedMetrics
                metrics={throughputMetrics}
                tokenLabel={tokenRateLabel}
                requestLabel={requestRateLabel}
                tokenShortLabel={tokenShortLabel}
                requestShortLabel={requestShortLabel}
                metricsTooltip={throughputMetricHint}
              />
            )}
            full
          >
            <RealtimeChartFrame loading={loading} emptyLabel={throughputEmptyLabel}>
              <Line data={throughputChartData} options={throughputOptions} plugins={THROUGHPUT_CHART_PLUGINS} />
            </RealtimeChartFrame>
          </RealtimeCard>

          {data.insights && <RealtimeDiagnostics insights={data.insights} labels={outcomeLabels} isDark={isDark} isMobile={isMobile} />}

          <div className={styles.overviewRealtimeResponseUsageRow}>
            <RealtimeCard
              title={t('usage_stats.analysis_latency_title')}
              metrics={latencyMetrics}
              className={styles.overviewRealtimeLatencyScatterCard}
            >
              <RealtimeChartFrame loading={loading} emptyLabel={latencyEmptyLabel}>
                <LatencyScatterChart diagnostics={latencyScatter} isDark={isDark} isMobile={isMobile} labels={latencyLabels} formatDuration={formatRealtimeDuration} />
              </RealtimeChartFrame>
            </RealtimeCard>

            <RealtimeCard title={t('usage_stats.overview_realtime_current_usage')} className={styles.overviewRealtimeCurrentUsageCard}>
              <div className={styles.overviewRealtimeDimensionTabs}>
                {dimensions.map((dimension) => (
                  <button
                    key={dimension.key}
                    type="button"
                    className={`${styles.overviewRealtimeDimensionTab} ${visibleDimension?.key === dimension.key ? styles.overviewRealtimeDimensionTabActive : ''}`.trim()}
                    onClick={() => setActiveDimension(dimension.key)}
                    aria-pressed={visibleDimension?.key === dimension.key}
                  >
                    {t(dimension.labelKey)}
                  </button>
                ))}
              </div>
              <RealtimeTokenShareRibbons key={visibleDimension?.key} items={visibleDimension?.items ?? []} loading={loading} />
            </RealtimeCard>
          </div>

          <RealtimeCard
            title={t('usage_stats.overview_realtime_cache_level')}
            metrics={metricChips(cacheValues, (value) => `${formatFixedTwoDecimals(value)}%`, averageLabel, latestLabel, trendLabel)}
            metricsTooltip={rollingMetricHint}
            full
          >
            <RealtimeChartFrame loading={loading} emptyLabel={cacheEmptyLabel}>
              <RealtimeCacheChart points={data.cache_level} labels={cacheLabels} isDark={isDark} isMobile={isMobile} />
            </RealtimeChartFrame>
          </RealtimeCard>
          </div>
        </>
      )}
    </div>
  );
}
