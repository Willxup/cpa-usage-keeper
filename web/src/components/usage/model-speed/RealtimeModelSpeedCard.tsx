import { useCallback, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import '@/lib/chartjs';
import type { ChartData, ChartOptions, Plugin } from 'chart.js';
import { Scatter } from 'react-chartjs-2';
import type { RealtimeModelSpeedPayload } from '@/lib/types';
import { formatCompactNumber } from '@/utils/usage';
import { getUsageChartTheme, USAGE_CHART_COMPOSITION_COLORS } from '@/utils/usage/chartConfig';
import { formatModelSpeedTimeRange, modelSpeedBucketIndex, modelSpeedMean, modelSpeedCombinedMean } from './analysisModelSpeed';
import { ModelSpeedPicker } from './ModelSpeedPicker';
import { ModelSpeedLegend } from './ModelSpeedLegend';
import { ModelSpeedTooltip } from './ModelSpeedTooltip';
import { useModelSpeedSelection } from './modelSpeedSelection';
import { MODEL_SPEED_ANIMATION_MS, useModelSpeedAnimation, type ModelSpeedRefreshSignal } from './modelSpeedAnimation';
import panelStyles from '../analysis/AnalysisPanel.module.scss';
import usageStyles from '@/pages/UsagePage.module.scss';
import styles from './ModelSpeedChart.module.scss';

type SpeedPoint = { x: number | null; y: number };
type Hover = { source: RealtimeModelSpeedPayload; model: string; half: number | null; x: number; y: number; touch: boolean };
const EMPTY_SPEED: RealtimeModelSpeedPayload = { available: false, series: [] };
const colorAt = (slot: number | undefined, isDark: boolean) => {
  const color = USAGE_CHART_COMPOSITION_COLORS[slot ?? 0];
  return isDark ? color.light : color.base;
};

export function RealtimeModelSpeedCard({ speed: response, loading, isDark, isMobile, timezone = 'UTC', preferenceScope = 'admin', queryKey = '', refreshSignal }: {
  speed?: RealtimeModelSpeedPayload;
  loading: boolean;
  isDark: boolean;
  isMobile: boolean;
  timezone?: string;
  preferenceScope?: string | null;
  queryKey?: string;
  refreshSignal?: ModelSpeedRefreshSignal;
}) {
  const { t, i18n } = useTranslation();
  const speed = response ?? EMPTY_SPEED;
  const initialLoading = loading && !response;
  const { selected, colors, candidates, choose, reset } = useModelSpeedSelection('realtime', preferenceScope, speed.series, queryKey, response !== undefined && !loading);
  const { reducedMotion, beforeDatasetsUpdate } = useModelSpeedAnimation(queryKey, response, refreshSignal);
  const theme = useMemo(() => getUsageChartTheme(isDark), [isDark]);
  const models = useMemo(() => new Map(speed.series.map((series) => [series.model, series])), [speed]);
  const locale = i18n?.language ?? 'en';
  const firstLabel = t('usage_stats.model_speed_first_half');
  const secondLabel = t('usage_stats.model_speed_second_half');
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const touchInput = useRef(false);
  const [hover, setHover] = useState<Hover | null>(null);
  const [pickerOpen, setPickerOpen] = useState(false);
  const dismiss = useCallback(() => setHover(null), []);
  const closeOnLeave = useCallback(() => { if (!touchInput.current) dismiss(); }, [dismiss]);
  const changePickerOpen = useCallback((open: boolean) => { setPickerOpen(open); if (open) dismiss(); }, [dismiss]);
  const radius = isMobile ? 4 : 5;
  const noDataLabel = t('usage_stats.no_data');
  const chartData = useMemo<ChartData<'scatter', SpeedPoint[]>>(() => ({
    datasets: selected.flatMap((model, row) => {
      const color = colorAt(colors[model], isDark);
      const series = models.get(model);
      return [series?.first_half, series?.second_half].map((sample, half) => ({
        id: JSON.stringify([model, half]),
        label: model,
        data: [{ x: modelSpeedMean(sample), y: row }],
        backgroundColor: half === 0 ? 'transparent' : color,
        borderColor: color,
        pointRadius: radius,
        pointHoverRadius: radius + 1,
        pointBorderWidth: 2,
      }));
    }),
  }), [selected, models, colors, isDark, radius]);

  const handlers = useMemo<Plugin<'scatter'>>(() => ({
    id: 'realtimeModelSpeed',
    beforeDatasetsUpdate,
    beforeDatasetsDraw(chart) {
      chart.ctx.save(); chart.ctx.lineWidth = 1.5;
      selected.forEach((model, row) => {
        const series = models.get(model);
        const first = modelSpeedMean(series?.first_half);
        const second = modelSpeedMean(series?.second_half);
        if (first === null && second === null) {
          chart.ctx.fillStyle = theme.textSecondary; chart.ctx.font = '11px Inter, system-ui, sans-serif'; chart.ctx.textBaseline = 'middle';
          chart.ctx.fillText(noDataLabel, chart.scales.x.getPixelForValue(0) + 6, chart.scales.y.getPixelForValue(row));
        }
        if (first === null || second === null) return;
        const start = chart.getDatasetMeta(row * 2).data[0];
        const end = chart.getDatasetMeta(row * 2 + 1).data[0];
        const left = Math.min(start.x, end.x) + radius + 1;
        const right = Math.max(start.x, end.x) - radius - 1;
        if (left >= right) return;
        chart.ctx.strokeStyle = colorAt(colors[model], isDark);
        chart.ctx.beginPath(); chart.ctx.moveTo(left, start.y); chart.ctx.lineTo(right, start.y); chart.ctx.stroke();
      });
      chart.ctx.restore();
    },
    afterEvent(chart, { event }) {
      if (pickerOpen || !speed.available || !selected.length) return;
      if (event.type === 'mouseout') { closeOnLeave(); return; }
      if (event.type !== 'mousemove' && event.type !== 'click') return;
      if (touchInput.current && event.type !== 'click') return;
      const { x, y } = event;
      const area = chart.chartArea;
      if (x === null || y === null || x < area.left || x > area.right || y < area.top || y > area.bottom) { closeOnLeave(); return; }
      const value = chart.scales.y.getValueForPixel(y);
      if (typeof value !== 'number') return;
      const row = modelSpeedBucketIndex(value, selected.length);
      const model = selected[row];
      const series = models.get(model);
      let half: number | null = null;
      let nearest = Infinity;
      [series?.first_half, series?.second_half].forEach((sample, index) => {
        if (modelSpeedMean(sample) === null) return;
        const point = chart.getDatasetMeta(row * 2 + index).data[0];
        if (!point) return;
        const distance = Math.hypot(x - point.x, y - point.y);
        if (distance <= radius + 3 && distance < nearest) { half = index; nearest = distance; }
      });
      canvasRef.current = chart.canvas;
      setHover((previous) => previous?.source === speed && previous.model === model && previous.half === half && previous.x === x && previous.y === y && previous.touch === touchInput.current
        ? previous : { source: speed, model, half, x, y, touch: touchInput.current });
    },
  }), [selected, models, colors, isDark, radius, speed, pickerOpen, closeOnLeave, theme, noDataLabel, beforeDatasetsUpdate]);
  const liveHandlers = useRef(handlers);
  useLayoutEffect(() => { liveHandlers.current = handlers; }, [handlers]);
  const plugin = useMemo<Plugin<'scatter'>>(() => ({
    id: 'realtimeModelSpeed',
    beforeDatasetsUpdate: (chart, args, options) => liveHandlers.current.beforeDatasetsUpdate?.(chart, args, options),
    beforeDatasetsDraw: (chart, args, options) => liveHandlers.current.beforeDatasetsDraw?.(chart, args, options),
    afterEvent: (chart, args, options) => liveHandlers.current.afterEvent?.(chart, args, options),
  }), []);
  const chartOptions = useMemo<ChartOptions<'scatter'>>(() => ({
    responsive: true, maintainAspectRatio: false,
    indexAxis: 'y',
    animation: reducedMotion ? false : { duration: MODEL_SPEED_ANIMATION_MS },
    transitions: { resize: { animation: { duration: reducedMotion ? 0 : MODEL_SPEED_ANIMATION_MS } } },
    layout: { padding: { top: 6, right: 8, bottom: 2 } },
    plugins: { legend: { display: false }, tooltip: { enabled: false } },
    scales: {
      x: { type: 'linear', beginAtZero: true, min: 0,
        grid: { color: theme.grid }, border: { color: theme.axis },
        title: { display: true, text: 't/s', color: theme.textSecondary, font: { size: 10 } },
        ticks: { color: theme.textSecondary, font: { size: 10 }, maxTicksLimit: isMobile ? 4 : 5, callback: (value) => formatCompactNumber(Number(value)) },
      },
      y: { type: 'linear', min: -0.5, max: Math.max(0.5, selected.length - 0.5), reverse: true,
        grid: { color: theme.grid, drawTicks: false }, border: { display: false },
        afterBuildTicks: (scale) => { scale.ticks = selected.map((_, index) => ({ value: index })); },
        ticks: { color: theme.textSecondary, font: { size: 10 }, autoSkip: false,
          callback: (value) => { const model = selected[Number(value)] ?? ''; const max = isMobile ? 12 : 22; return model.length > max ? `${model.slice(0, max - 1)}…` : model; },
        },
      },
    },
  }), [theme, isMobile, selected, reducedMotion]);
  const visibleHover = !initialLoading && !pickerOpen && speed.available && hover?.source === speed && selected.includes(hover.model) ? hover : null;
  const hoveredModel = visibleHover ? models.get(visibleHover.model) : null;
  const tooltipRows = visibleHover ? [
    { sample: hoveredModel?.first_half, label: firstLabel, start: speed.start, end: speed.mid },
    { sample: hoveredModel?.second_half, label: secondLabel, start: speed.mid, end: speed.end },
  ].map(({ sample, label, start, end }, half) => {
    const mean = modelSpeedMean(sample);
    return { model: label, color: colorAt(colors[visibleHover.model], isDark),
      highlighted: visibleHover.half === half,
      detail: mean === null ? t('usage_stats.no_data') : `${mean.toFixed(1)} t/s`,
      secondary: `${formatModelSpeedTimeRange(start, end, timezone, locale, 'seconds', [speed.start, speed.mid, speed.end].filter((value): value is string => Boolean(value)))} · ${t('usage_stats.model_speed_samples', { samples: (sample?.speed_sample_count ?? 0).toLocaleString(locale) })}` };
  }) : [];
  const legendItems = selected.map((model) => {
    const series = models.get(model);
    return { model, color: colorAt(colors[model], isDark), mean: modelSpeedCombinedMean([series?.first_half, series?.second_half]) };
  });

  return <section className={`${usageStyles.overviewRealtimeCard} ${styles.card} keeper-card-surface`} data-realtime-model-speed aria-busy={loading}>
    <div className={`${styles.header} ${styles.realtimeHeader} keeper-card-heading`}>
      <div className="keeper-card-title-track"><h3 className="keeper-card-title">{t('usage_stats.realtime_model_speed_title')}</h3></div>
      <ModelSpeedPicker candidates={candidates} selected={selected} onChange={choose} onReset={reset} disabled={initialLoading} onOpenChange={changePickerOpen} />
    </div>
    <div className={styles.phases}>
      <span><i className={styles.phaseDot} aria-hidden="true" />{firstLabel}</span>
      <span><i className={`${styles.phaseDot} ${styles.phaseFilled}`} aria-hidden="true" />{secondLabel}</span>
    </div>
    <div className={`${styles.body} ${styles.realtimeBody}`}>
      {initialLoading ? <div className={`${panelStyles.emptyState} ${styles.empty}`}>{t('common.loading')}</div>
        : !speed.available ? <div className={`${panelStyles.emptyState} ${styles.empty}`}>{t('usage_stats.model_speed_unavailable')}</div>
        : selected.length === 0 ? <div className={`${panelStyles.emptyState} ${styles.empty}`}>{t(candidates.length === 0 ? 'usage_stats.no_data' : 'usage_stats.model_speed_empty_selection')}</div>
        : <div className={styles.chart} onPointerDown={(event) => { touchInput.current = event.pointerType === 'touch'; }}
          onPointerMove={(event) => { if (event.pointerType === 'mouse') touchInput.current = false; }}>
          <Scatter data={chartData} datasetIdKey="id" options={chartOptions} plugins={[plugin]} />
        </div>}
      <ModelSpeedLegend items={legendItems} />
    </div>
    {visibleHover && <ModelSpeedTooltip canvasRef={canvasRef} x={visibleHover.x} y={visibleHover.y} touch={visibleHover.touch} theme={theme}
      title={visibleHover.model} rows={tooltipRows} dismiss={dismiss} />}
  </section>;
}
