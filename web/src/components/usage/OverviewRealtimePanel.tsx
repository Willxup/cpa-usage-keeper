import { useMemo, useState, type ComponentProps, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Base, Line, type LineConfig } from '@ant-design/charts';
import type {
  OverviewRealtimeBlock,
  OverviewRealtimeWindow,
  RealtimeResponseAveragePoint,
  RealtimeResponseParticle,
  RealtimeUsageTopItem,
} from '@/lib/types';
import {
  formatCompactNumber,
  formatDurationMs,
  formatFixedTwoDecimals,
  formatPerMinuteValue,
  formatUsd,
} from '@/utils/usage';
import { SectionHeader } from '@/components/layout';
import { LoadingSpinner } from '@/components/ui/LoadingSpinner';
import { getChartTheme } from '@/lib/chartTheme';
import styles from './UsageOverview.module.scss';

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

type RealtimeLineDatum = { label: string; value: number | null };
type ResponseDistributionDatum = { x: number; y: number | null };
type ResponseDistributionParticleDatum = { x: number; y: number; count: number };
type ResponseDistributionXBounds = { min: number; max: number };
type ResponseDistributionConfig = ComponentProps<typeof Base>;

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

const REALTIME_DURATION_UNITS = {
  d: 'd',
  h: 'h',
  m: 'm',
  s: 's',
  ms: 'ms',
} as const;

const emptyRealtime = (window: OverviewRealtimeWindow): OverviewRealtimeBlock => ({
  window,
  bucket_seconds: window === '30m' ? 60 : window === '60m' ? 120 : 30,
  token_velocity: [],
  response_level: [],
  response_distribution: {
    ttft: {
      average_line: [],
      particles: [],
    },
    latency: {
      average_line: [],
      particles: [],
    },
  },
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

const formatRealtimeTokenRate = (value: number) => `${formatCompactNumber(value)}/min`;

const formatRealtimeDuration = (value: number) => formatDurationMs(value, {
  maxUnits: 2,
  locale: 'en-US',
  unitLabels: REALTIME_DURATION_UNITS,
});

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

function buildRealtimeLineData(labels: string[], values: Array<number | null>): RealtimeLineDatum[] {
  return Array.from({ length: Math.max(labels.length, values.length) }, (_, index) => ({
    label: labels[index] ?? '',
    value: values[index] ?? null,
  }));
}

function buildRealtimeLineConfig(
  data: RealtimeLineDatum[],
  label: string,
  color: string,
  isDark: boolean,
  isMobile: boolean,
  valueFormatter: (value: number) => string,
  options: { yTickCount?: number } = {},
): LineConfig {
  const theme = getChartTheme(isDark);
  return {
    data,
    xField: 'label',
    yField: 'value',
    shapeField: 'smooth',
    animate: false,
    legend: false,
    theme: { type: isDark ? 'classicDark' : 'classic' },
    scale: {
      y: {
        type: 'linear',
        domainMin: 0,
        nice: true,
      },
    },
    axis: {
      x: {
        title: false,
        grid: false,
        tickCount: isMobile ? 5 : 8,
        labelFill: theme.textSecondary,
        labelFontSize: isMobile ? 10 : 11,
        line: true,
        lineStroke: theme.axis,
        tickStroke: theme.axis,
      },
      y: {
        title: false,
        grid: true,
        tickCount: options.yTickCount,
        labelFormatter: (value: number) => valueFormatter(Number(value)),
        labelFill: theme.textSecondary,
        labelFontSize: isMobile ? 10 : 11,
        gridStroke: theme.grid,
        line: true,
        lineStroke: theme.axis,
        tickStroke: theme.axis,
      },
    },
    style: {
      stroke: color,
      lineWidth: isMobile ? 1.6 : 2,
    },
    area: {
      style: {
        fill: `${color}24`,
        fillOpacity: 1,
      },
      tooltip: false,
    },
    tooltip: {
      title: 'label',
      items: [{
        name: label,
        color,
        field: 'value',
        valueFormatter: (value: number | null) => value == null ? '--' : valueFormatter(Number(value)),
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

function responseDistributionAveragePoints(
  points: RealtimeResponseAveragePoint[] | null | undefined,
  fallbackPoints: Array<{ bucket: string; value?: number | null }>,
): RealtimeResponseAveragePoint[] {
  if (points && points.length > 0) return points;
  return fallbackPoints.map((point) => ({
    bucket: point.bucket,
    avg_ms: point.value ?? null,
  }));
}

function responseDistributionValues(points: RealtimeResponseAveragePoint[] | null | undefined): Array<number | null> {
  return (points ?? []).filter(Boolean).map((point) => {
    if (point.avg_ms == null) return null;
    const value = safeNumber(point.avg_ms);
    return value > 0 ? value : null;
  });
}

function parseResponseDistributionTime(value: string | null | undefined): number | null {
  const parsed = Date.parse(value ?? '');
  return Number.isFinite(parsed) ? parsed : null;
}

function responseDistributionAverageData(points: RealtimeResponseAveragePoint[] | null | undefined): ResponseDistributionDatum[] {
  return (points ?? []).filter(Boolean).map((point) => {
    const x = parseResponseDistributionTime(point.bucket);
    if (x == null) return null;
    if (point.avg_ms == null) return { x, y: null };
    const value = safeNumber(point.avg_ms);
    return { x, y: value > 0 ? value : null };
  }).filter((point): point is ResponseDistributionDatum => point !== null);
}

function responseDistributionParticleData(particles: RealtimeResponseParticle[] | null | undefined): ResponseDistributionParticleDatum[] {
  return (particles ?? []).filter(Boolean).map((point) => {
    const x = parseResponseDistributionTime(point.timestamp ?? point.bucket);
    if (x == null) return null;
    return {
      x,
      y: safeNumber(point.ms),
      count: Math.max(1, safeNumber(point.count)),
    };
  }).filter((point): point is ResponseDistributionParticleDatum => Boolean(point && point.y > 0));
}

function responseDistributionXBounds(data: OverviewRealtimeBlock): ResponseDistributionXBounds | undefined {
  const min = parseResponseDistributionTime(data.window_start);
  const max = parseResponseDistributionTime(data.window_end);
  if (min != null && max != null && max > min) {
    return { min, max };
  }

  const bucketSeconds = safeNumber(data.bucket_seconds);
  if (bucketSeconds <= 0) return undefined;
  const bucketStarts = [
    ...data.token_velocity,
    ...data.response_level,
    ...data.request_level,
    ...data.cache_level,
    ...data.response_distribution.ttft.average_line,
    ...data.response_distribution.latency.average_line,
  ].map((point) => parseResponseDistributionTime(point.bucket))
    .filter((value): value is number => value != null);
  if (bucketStarts.length === 0) return undefined;
  const minBucket = Math.min(...bucketStarts);
  const maxBucket = Math.max(...bucketStarts) + bucketSeconds * 1000;
  return maxBucket > minBucket ? { min: minBucket, max: maxBucket } : undefined;
}

function responseParticleRadius(count: number, isMobile: boolean): number {
  // 分布点只表示样本位置，密度由点数体现，避免放大成气泡图。
  const normalized = Math.min(Math.max(1, count), 6);
  return (isMobile ? 1.15 : 1.35) + normalized * 0.08;
}

function buildResponseDistributionConfig(
  averageLabel: string,
  particleLabel: string,
  averageData: ResponseDistributionDatum[],
  particles: ResponseDistributionParticleDatum[],
  color: string,
  isDark: boolean,
  isMobile: boolean,
  xBounds: ResponseDistributionXBounds | undefined,
  timezone?: string,
): ResponseDistributionConfig {
  const theme = getChartTheme(isDark);
  const yBounds = responseDistributionLogAxisBounds(averageData, particles);
  const axis = {
    x: {
      title: false,
      grid: false,
      tickCount: isMobile ? 5 : 8,
      labelFormatter: (value: number) => formatResponseDistributionTick(Number(value), timezone),
      labelFill: theme.textSecondary,
      labelFontSize: isMobile ? 10 : 11,
      line: true,
      lineStroke: theme.axis,
      tickStroke: theme.axis,
    },
    y: {
      title: false,
      grid: true,
      tickCount: 5,
      labelFormatter: (value: number) => formatRealtimeDuration(Number(value)),
      labelFill: theme.textSecondary,
      labelFontSize: isMobile ? 10 : 11,
      gridStroke: theme.grid,
      line: true,
      lineStroke: theme.axis,
      tickStroke: theme.axis,
    },
  };
  return {
    animate: false,
    legend: false,
    theme: { type: isDark ? 'classicDark' : 'classic' },
    scale: {
      x: {
        type: 'linear',
        nice: false,
        ...(xBounds ? { domainMin: xBounds.min, domainMax: xBounds.max } : {}),
      },
      y: {
        type: 'log',
        domainMin: yBounds.min,
        domainMax: yBounds.max,
        nice: false,
      },
    },
    axis,
    interaction: {
      tooltip: {
        shared: false,
        crosshairs: true,
        marker: true,
      },
    },
    children: [
      {
        type: 'line',
        data: averageData,
        encode: { x: 'x', y: 'y' },
        style: {
          shape: 'smooth',
          stroke: color,
          lineWidth: isMobile ? 1.8 : 2.2,
        },
        tooltip: {
          title: (datum: ResponseDistributionDatum) => formatResponseDistributionTick(datum.x, timezone),
          items: [(datum: ResponseDistributionDatum) => ({
            name: averageLabel,
            color,
            value: datum.y == null ? '--' : formatRealtimeDuration(datum.y),
          })],
        },
      },
      {
        type: 'point',
        data: particles,
        encode: { x: 'x', y: 'y' },
        style: {
          fill: `${color}66`,
          stroke: 'transparent',
          r: (datum: ResponseDistributionParticleDatum) => responseParticleRadius(datum.count, isMobile),
        },
        state: {
          active: {
            r: (datum: ResponseDistributionParticleDatum) => responseParticleRadius(datum.count, isMobile) + 1.1,
          },
        },
        tooltip: {
          title: (datum: ResponseDistributionParticleDatum) => formatResponseDistributionTick(datum.x, timezone),
          items: [(datum: ResponseDistributionParticleDatum) => ({
            name: particleLabel,
            color,
            value: `${formatRealtimeDuration(datum.y)} (${formatCompactNumber(datum.count)})`,
          })],
        },
      },
    ],
  };
}

function formatResponseDistributionTick(value: number, timezone?: string): string {
  if (!Number.isFinite(value)) return '';
  return formatBucketLabel(new Date(value).toISOString(), timezone);
}

function responseDistributionLogAxisBounds(averageData: ResponseDistributionDatum[] | null | undefined, particles: ResponseDistributionParticleDatum[] | null | undefined): { min: number; max: number } {
  let minValue = Number.POSITIVE_INFINITY;
  let maxValue = 0;
  for (const point of averageData ?? []) {
    const value = point.y;
    if (value == null || !Number.isFinite(value) || value <= 0) continue;
    minValue = Math.min(minValue, value);
    maxValue = Math.max(maxValue, value);
  }
  for (const particle of particles ?? []) {
    if (!particle) continue;
    const value = particle.y;
    if (!Number.isFinite(value) || value <= 0) continue;
    minValue = Math.min(minValue, value);
    maxValue = Math.max(maxValue, value);
  }
  if (!Number.isFinite(minValue) || maxValue <= 0) {
    return { min: 1, max: 10 };
  }
  return {
    min: Math.max(1, Math.floor(minValue / 1.35)),
    max: Math.max(10, Math.ceil(maxValue * 1.18)),
  };
}

function RealtimeCard({
  title,
  metrics,
  children,
  full = false,
  compact = false,
  className,
  metricsTooltip,
}: {
  title: string;
  metrics?: RealtimeMetric[];
  children: ReactNode;
  full?: boolean;
  compact?: boolean;
  className?: string;
  metricsTooltip?: string;
}) {
  const cardClassName = [
    styles.overviewRealtimeCard,
    full ? styles.overviewRealtimeCardFull : '',
    compact ? styles.overviewRealtimeCardCompact : '',
    className ?? '',
  ].filter(Boolean).join(' ');
  return (
    <section className={cardClassName}>
      <SectionHeader
        className={styles.overviewRealtimeCardHeader}
        headingLevel={3}
        title={title}
        actions={metrics && metrics.length > 0 ? (
          <div className={styles.overviewRealtimeMetrics}>
            {metrics.map((metric) => (
              <span
                key={metric.label}
                className={`${styles.overviewRealtimeMetric} ${metric.tone === 'up' ? styles.overviewRealtimeMetricUp : metric.tone === 'down' ? styles.overviewRealtimeMetricDown : metric.tone === 'flat' ? styles.overviewRealtimeMetricFlat : ''}`.trim()}
                title={metricsTooltip}
                aria-label={metricsTooltip ? `${metric.label} ${metricsTooltip}` : undefined}
              >
                <span className={styles.overviewRealtimeMetricLabel}>{metric.label}</span>
                <span className={styles.overviewRealtimeMetricValue}>{metric.value}</span>
              </span>
            ))}
          </div>
        ) : undefined}
      />
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

function UsageMetaPill({ label, value }: { label: string; value: string }) {
  return (
    <span className={styles.overviewRealtimeUsageMetaPill}>
      <span className={styles.overviewRealtimeUsageMetaLabel}>{label}</span>
      <span className={styles.overviewRealtimeUsageMetaValue}>{value}</span>
    </span>
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
  const labels = useMemo(() => data.token_velocity.map((point) => formatBucketLabel(point.bucket, data.timezone ?? timezone)), [data.timezone, data.token_velocity, timezone]);

  const tokenValues = useMemo(() => data.token_velocity.map((point) => safeNumber(point.tokens_per_minute)), [data.token_velocity]);
  const requestValues = useMemo(() => data.request_level.map((point) => safeNumber(point.requests_per_minute)), [data.request_level]);
  const cacheValues = useMemo(() => data.cache_level.map((point) => point.cache_read_rate == null ? null : safeNumber(point.cache_read_rate)), [data.cache_level]);
  const responseTimezone = data.timezone ?? timezone;
  const ttftAveragePoints = useMemo(() => responseDistributionAveragePoints(
    data.response_distribution.ttft.average_line,
    data.response_level.map((point) => ({ bucket: point.bucket, value: point.ttft_p95_ms })),
  ), [data.response_distribution.ttft.average_line, data.response_level]);
  const latencyAveragePoints = useMemo(() => responseDistributionAveragePoints(
    data.response_distribution.latency.average_line,
    data.response_level.map((point) => ({ bucket: point.bucket, value: point.latency_p95_ms })),
  ), [data.response_distribution.latency.average_line, data.response_level]);
  const ttftAverageValues = useMemo(() => responseDistributionValues(ttftAveragePoints), [ttftAveragePoints]);
  const latencyAverageValues = useMemo(() => responseDistributionValues(latencyAveragePoints), [latencyAveragePoints]);
  const ttftAverageChartData = useMemo(() => responseDistributionAverageData(ttftAveragePoints), [ttftAveragePoints]);
  const latencyAverageChartData = useMemo(() => responseDistributionAverageData(latencyAveragePoints), [latencyAveragePoints]);
  const ttftParticleValues = useMemo(() => responseDistributionParticleData(data.response_distribution.ttft.particles), [data.response_distribution.ttft.particles]);
  const latencyParticleValues = useMemo(() => responseDistributionParticleData(data.response_distribution.latency.particles), [data.response_distribution.latency.particles]);
  const distributionXBounds = useMemo(() => responseDistributionXBounds(data), [data]);
  const tokenEmptyLabel = data.token_velocity.length === 0 ? t('usage_stats.overview_realtime_token_empty') : undefined;
  const requestEmptyLabel = data.request_level.length === 0 ? t('usage_stats.overview_realtime_request_empty') : undefined;
  const ttftEmptyLabel = !hasFiniteNumber(ttftAverageValues) && ttftParticleValues.length === 0 ? t('usage_stats.overview_realtime_ttft_empty') : undefined;
  const latencyEmptyLabel = !hasFiniteNumber(latencyAverageValues) && latencyParticleValues.length === 0 ? t('usage_stats.overview_realtime_latency_empty') : undefined;
  const cacheEmptyLabel = !hasFiniteNumber(cacheValues) ? t('usage_stats.overview_realtime_cache_empty') : undefined;

  const latestLabel = t('usage_stats.overview_realtime_latest');
  const averageLabel = t('usage_stats.overview_realtime_average');
  const trendLabel = t('usage_stats.overview_realtime_trend');
  const rollingMetricHint = t('usage_stats.overview_realtime_rolling_metric_hint');
  const chartColors = useMemo(() => {
    const series = getChartTheme(isDark).series;
    return {
      token: series.violet.stroke,
      ttft: series.orange.stroke,
      latency: series.cyan.stroke,
      request: series.blue.stroke,
      cache: series.teal.stroke,
    } as const;
  }, [isDark]);

  const tokenChartData = useMemo(() => buildRealtimeLineData(labels, tokenValues), [labels, tokenValues]);
  const requestChartData = useMemo(() => buildRealtimeLineData(labels, requestValues), [labels, requestValues]);
  const cacheChartData = useMemo(() => buildRealtimeLineData(labels, cacheValues), [cacheValues, labels]);
  const tokenChartConfig = useMemo(() => buildRealtimeLineConfig(
    tokenChartData,
    t('usage_stats.overview_realtime_tpm'),
    chartColors.token,
    isDark,
    isMobile,
    formatCompactNumber,
  ), [chartColors.token, isDark, isMobile, t, tokenChartData]);
  const requestChartConfig = useMemo(() => buildRealtimeLineConfig(
    requestChartData,
    t('usage_stats.overview_realtime_rpm'),
    chartColors.request,
    isDark,
    isMobile,
    formatCompactNumber,
  ), [chartColors.request, isDark, isMobile, requestChartData, t]);
  const cacheChartConfig = useMemo(() => buildRealtimeLineConfig(
    cacheChartData,
    t('usage_stats.overview_realtime_cache_rate'),
    chartColors.cache,
    isDark,
    isMobile,
    (value) => `${formatFixedTwoDecimals(value)}%`,
    { yTickCount: 5 },
  ), [cacheChartData, chartColors.cache, isDark, isMobile, t]);
  const ttftDistributionConfig = useMemo(() => buildResponseDistributionConfig(
    t('usage_stats.overview_realtime_ttft_average'),
    t('usage_stats.overview_realtime_ttft_distribution'),
    ttftAverageChartData,
    ttftParticleValues,
    chartColors.ttft,
    isDark,
    isMobile,
    distributionXBounds,
    responseTimezone,
  ), [chartColors.ttft, distributionXBounds, isDark, isMobile, responseTimezone, t, ttftAverageChartData, ttftParticleValues]);
  const latencyDistributionConfig = useMemo(() => buildResponseDistributionConfig(
    t('usage_stats.overview_realtime_latency_average'),
    t('usage_stats.overview_realtime_latency_distribution'),
    latencyAverageChartData,
    latencyParticleValues,
    chartColors.latency,
    isDark,
    isMobile,
    distributionXBounds,
    responseTimezone,
  ), [chartColors.latency, distributionXBounds, isDark, isMobile, latencyAverageChartData, latencyParticleValues, responseTimezone, t]);
  const ttftMetrics = useMemo(() => metricChips(ttftAverageValues, formatRealtimeDuration, averageLabel, latestLabel, trendLabel, {
      invertTone: true,
    }), [averageLabel, latestLabel, trendLabel, ttftAverageValues]);
  const latencyMetrics = useMemo(() => metricChips(latencyAverageValues, formatRealtimeDuration, averageLabel, latestLabel, trendLabel, {
      invertTone: true,
    }), [averageLabel, latencyAverageValues, latestLabel, trendLabel]);

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
      <SectionHeader
        headingLevel={2}
        title={t('usage_stats.overview_realtime_section_title')}
        actions={(
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
        )}
      />

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
          <div className={styles.overviewRealtimeGrid}>
          <RealtimeCard
            title={t('usage_stats.overview_realtime_token_velocity')}
            metrics={metricChips(tokenValues, formatRealtimeTokenRate, averageLabel, latestLabel, trendLabel)}
            metricsTooltip={rollingMetricHint}
            full
          >
            <RealtimeChartFrame loading={loading} emptyLabel={tokenEmptyLabel}>
              <Line {...tokenChartConfig} />
            </RealtimeChartFrame>
          </RealtimeCard>

          <div className={styles.overviewRealtimeResponseUsageRow}>
            <div className={styles.overviewRealtimeResponseStack}>
              <RealtimeCard
                title={t('usage_stats.overview_realtime_ttft_distribution')}
                metrics={ttftMetrics}
                metricsTooltip={rollingMetricHint}
                compact
              >
                <RealtimeChartFrame loading={loading} emptyLabel={ttftEmptyLabel}>
                  <Base {...ttftDistributionConfig} />
                </RealtimeChartFrame>
              </RealtimeCard>

              <RealtimeCard
                title={t('usage_stats.overview_realtime_latency_distribution')}
                metrics={latencyMetrics}
                metricsTooltip={rollingMetricHint}
                compact
              >
                <RealtimeChartFrame loading={loading} emptyLabel={latencyEmptyLabel}>
                  <Base {...latencyDistributionConfig} />
                </RealtimeChartFrame>
              </RealtimeCard>
            </div>

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
              <div className={styles.overviewRealtimeUsageList} aria-busy={loading}>
                {(visibleDimension?.items ?? []).length === 0 ? (
                  <div className={styles.overviewRealtimeEmpty}>{t('usage_stats.overview_realtime_usage_empty')}</div>
                ) : (
                  visibleDimension?.items.map((item) => (
                    <div key={item.key} className={styles.overviewRealtimeUsageItem}>
                      <div className={styles.overviewRealtimeUsageTopline}>
                        <span className={styles.overviewRealtimeUsageLabel} title={item.label}>{item.label}</span>
                        <span className={styles.overviewRealtimeUsageShare}>{formatFixedTwoDecimals(safeNumber(item.share))}%</span>
                      </div>
                      <div className={styles.overviewRealtimeUsageTrack}>
                        {safeNumber(item.share) > 0 && (
                          <span className={styles.overviewRealtimeUsageBar} style={{ width: `${Math.max(0, Math.min(100, safeNumber(item.share)))}%` }} />
                        )}
                      </div>
                      <div className={styles.overviewRealtimeUsageMeta}>
                        <UsageMetaPill label={t('usage_stats.overview_realtime_tokens_label')} value={formatCompactNumber(item.tokens)} />
                        <UsageMetaPill label={t('usage_stats.overview_realtime_requests_label')} value={item.requests.toLocaleString()} />
                        {typeof item.cost === 'number' && <UsageMetaPill label={t('usage_stats.overview_realtime_cost_label')} value={formatUsd(item.cost)} />}
                      </div>
                    </div>
                  ))
                )}
              </div>
            </RealtimeCard>
          </div>

          <RealtimeCard
            title={t('usage_stats.overview_realtime_request_level')}
            metrics={metricChips(requestValues, formatPerMinuteValue, averageLabel, latestLabel, trendLabel)}
            metricsTooltip={rollingMetricHint}
          >
            <RealtimeChartFrame loading={loading} emptyLabel={requestEmptyLabel}>
              <Line {...requestChartConfig} />
            </RealtimeChartFrame>
          </RealtimeCard>

          <RealtimeCard
            title={t('usage_stats.overview_realtime_cache_level')}
            metrics={metricChips(cacheValues, (value) => `${formatFixedTwoDecimals(value)}%`, averageLabel, latestLabel, trendLabel)}
            metricsTooltip={rollingMetricHint}
          >
            <RealtimeChartFrame loading={loading} emptyLabel={cacheEmptyLabel}>
              <Line {...cacheChartConfig} />
            </RealtimeChartFrame>
          </RealtimeCard>
          </div>
        </>
      )}
    </div>
  );
}
