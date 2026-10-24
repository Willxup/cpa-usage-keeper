import { useCallback, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import '@/lib/chartjs';
import type { Chart, ChartData, ChartOptions, Plugin, PointElement } from 'chart.js';
import { color } from 'chart.js/helpers';
import { Scatter } from 'react-chartjs-2';
import type { AnalysisModelSpeedPayload, AnalysisResponse } from '@/lib/types';
import { formatCompactNumber } from '@/utils/usage';
import { getUsageChartTheme, USAGE_CHART_COMPOSITION_COLORS } from '@/utils/usage/chartConfig';
import { formatModelSpeedTimeRange, mergeModelSpeedBuckets, modelSpeedBucketIndex, modelSpeedMean, modelSpeedCombinedMean, modelSpeedTooltipRows } from './analysisModelSpeed';
import { ModelSpeedPicker } from './ModelSpeedPicker';
import { ModelSpeedLegend } from './ModelSpeedLegend';
import { ModelSpeedTooltip } from './ModelSpeedTooltip';
import { useModelSpeedSelection } from './modelSpeedSelection';
import { MODEL_SPEED_ANIMATION_MS, useModelSpeedAnimation, type ModelSpeedRefreshSignal } from './modelSpeedAnimation';
import { formatBucketLabels } from '../analysis/analysisTimeLabels';
import { useAnalysisHighlight } from '../analysis/analysisRanking';
import panelStyles from '../analysis/AnalysisPanel.module.scss';
import styles from './ModelSpeedChart.module.scss';

type SpeedPoint = { x: number; y: number | null };
type Hover = { source: AnalysisModelSpeedPayload; bucket: number; model: string | null; x: number; y: number; touch: boolean };
type Highlight = { source: AnalysisModelSpeedPayload; selected: readonly string[]; datasetIndex: number; index: number };
const EMPTY_SPEED: AnalysisModelSpeedPayload = { buckets: [], series: [] };
const colorAt = (slot: number | undefined, isDark: boolean) => {
  const color = USAGE_CHART_COMPOSITION_COLORS[slot ?? 0];
  return isDark ? color.light : color.base;
};

export function AnalysisModelSpeedCard({ speed: response, granularity = 'hourly', loading, isDark, isMobile, timezone = 'UTC', preferenceScope = 'admin', queryKey = '', refreshSignal }: {
  speed?: AnalysisModelSpeedPayload;
  granularity?: AnalysisResponse['granularity'];
  loading: boolean;
  isDark: boolean;
  isMobile: boolean;
  timezone?: string;
  preferenceScope?: string | null;
  queryKey?: string;
  refreshSignal?: ModelSpeedRefreshSignal;
}) {
  const { t, i18n } = useTranslation();
  const speed = useMemo(() => mergeModelSpeedBuckets(response ?? EMPTY_SPEED, isMobile ? 4 : 8), [response, isMobile]);
  const initialLoading = loading && !response;
  const { selected, colors, candidates, choose, reset } = useModelSpeedSelection('analysis', preferenceScope, speed.series, queryKey, response !== undefined && !initialLoading);
  const { reducedMotion, beforeDatasetsUpdate } = useModelSpeedAnimation(queryKey, response, refreshSignal);
  const theme = useMemo(() => getUsageChartTheme(isDark), [isDark]);
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const chartRef = useRef<Chart<'scatter'> | null>(null);
  const highlighted = useRef<Highlight | null>(null);
  const suppressCanvasReplay = useRef(false);
  const touchInput = useRef(false);
  const [hover, setHover] = useState<Hover | null>(null);
  const [pickerOpen, setPickerOpen] = useState(false);
  const legendHighlight = useAnalysisHighlight(JSON.stringify([queryKey, selected]), { resetOnScopeChange: true });
  const activeModel = !initialLoading && !pickerOpen && selected.includes(legendHighlight.active ?? '') ? legendHighlight.active : null;
  const locale = i18n?.language ?? 'en';
  const models = useMemo(() => new Map(speed.series.map((series) => [series.model, series])), [speed]);
  const labels = useMemo(() => formatBucketLabels(speed.buckets.map((bucket) => bucket.start), granularity, timezone), [speed, granularity, timezone]);
  const chartData = useMemo<ChartData<'scatter', SpeedPoint[]>>(() => ({
    datasets: selected.map((model, modelIndex) => ({
      label: model,
      data: speed.buckets.map((_, bucket) => ({
        x: bucket + (selected.length > 1 ? (modelIndex / (selected.length - 1) - 0.5) * 0.6 : 0),
        y: modelSpeedMean(models.get(model)?.samples[bucket]),
      })),
      backgroundColor: activeModel && activeModel !== model ? color(colorAt(colors[model], isDark)).alpha(0.25).rgbString() : colorAt(colors[model], isDark),
      borderColor: activeModel && activeModel !== model ? color(colorAt(colors[model], isDark)).alpha(0.25).rgbString() : colorAt(colors[model], isDark),
      pointRadius: (isMobile ? 2.5 : 4) + (activeModel === model ? 2 : 0),
      pointHoverRadius: isMobile ? 4.5 : 6,
      pointHoverBackgroundColor: colorAt(colors[model], isDark),
      pointHoverBorderColor: theme.tooltipBg,
      pointHoverBorderWidth: 2,
      borderWidth: activeModel === model ? 2.5 : 1,
    })),
  }), [selected, speed, models, colors, isDark, isMobile, theme.tooltipBg, activeModel]);
  const dismiss = useCallback(() => {
    highlighted.current = null;
    const chart = chartRef.current;
    if (chart?.canvas) { chart.setActiveElements([]); chart.render(); }
    setHover(null);
  }, []);
  const interactWithLegend = useCallback(() => {
    suppressCanvasReplay.current = true;
    dismiss();
  }, [dismiss]);
  const closeOnLeave = useCallback(() => { if (!touchInput.current) dismiss(); }, [dismiss]);
  const changePickerOpen = useCallback((open: boolean) => { setPickerOpen(open); if (open) dismiss(); }, [dismiss]);

  // 命中整个显示时间组，包含点之间的空白与无样本模型；不依赖 scatter 最近单点。
  const handlers = useMemo<Plugin<'scatter'>>(() => ({
    id: 'analysisModelSpeed',
    beforeDatasetsUpdate(chart) {
      chartRef.current = chart;
      if (highlighted.current && (highlighted.current.source !== speed || highlighted.current.selected !== selected)) {
        highlighted.current = null;
        chart.setActiveElements([]);
      }
      beforeDatasetsUpdate(chart);
    },
    beforeDatasetsDraw(chart) {
      const zero = chart.scales.y.getPixelForValue(0);
      chart.ctx.save();
      const hit = highlighted.current?.source === speed && highlighted.current.selected === selected ? highlighted.current : null;
      chart.data.datasets.forEach((dataset, datasetIndex) => {
        const metadata = chart.getDatasetMeta(datasetIndex);
        chart.ctx.strokeStyle = String(dataset.borderColor);
        metadata.data.forEach((point, index) => {
          if (modelSpeedMean(models.get(selected[datasetIndex])?.samples[index]) === null) return;
          // Read animated point styles so stems fade and thicken together with their nodes.
          const options = (point as PointElement).options;
          chart.ctx.strokeStyle = options?.backgroundColor ?? String(dataset.borderColor);
          chart.ctx.lineWidth = hit?.datasetIndex === datasetIndex && hit.index === index ? 3 : (options?.borderWidth ?? 1) + 0.5;
          chart.ctx.beginPath(); chart.ctx.moveTo(point.x, zero); chart.ctx.lineTo(point.x, point.y); chart.ctx.stroke();
        });
      });
      chart.ctx.restore();
    },
    afterEvent(chart, args) {
      const { event } = args;
      chartRef.current = chart;
      // Updating legend styles can replay a stale canvas event even when no point was active.
      if (args.replay && suppressCanvasReplay.current) {
        chart.setActiveElements([]);
        args.changed = true;
        return;
      }
      const syncActive = () => {
        const hit = highlighted.current;
        chart.setActiveElements(hit ? [{ datasetIndex: hit.datasetIndex, index: hit.index }] : []);
        args.changed = true;
      };
      if (pickerOpen || !selected.length || !speed.buckets.length) { chart.setActiveElements([]); args.changed = true; return; }
      if (event.type === 'mouseout') { closeOnLeave(); syncActive(); return; }
      if (event.type !== 'mousemove' && event.type !== 'click') return;
      if (touchInput.current && event.type !== 'click') { syncActive(); return; }
      if (!args.replay) suppressCanvasReplay.current = false;
      const { x, y } = event;
      const area = chart.chartArea;
      if (x === null || y === null || x < area.left || x > area.right || y < area.top || y > area.bottom) { closeOnLeave(); syncActive(); return; }
      const value = chart.scales.x.getValueForPixel(x);
      if (typeof value !== 'number') return;
      canvasRef.current = chart.canvas;
      const bucket = modelSpeedBucketIndex(value, speed.buckets.length);
      const zero = chart.scales.y.getPixelForValue(0);
      let hitIndex: number | null = null;
      let nearest = Infinity;
      // Tooltip 命中整组；高亮单独按当前圆点或竖线几何命中，空白与缺样本不猜测最近模型。
      selected.forEach((model, datasetIndex) => {
        if (modelSpeedMean(models.get(model)?.samples[bucket]) === null) return;
        const point = chart.getDatasetMeta(datasetIndex).data[bucket] as PointElement | undefined;
        if (!point || point.skip) return;
        const dotDistance = Math.hypot(x - point.x, y - point.y);
        const dx = Math.abs(x - point.x);
        const radius = isMobile ? 2.5 : 4;
        const onDot = dotDistance <= radius + 3;
        const onStem = dx <= 3 && y >= Math.min(point.y, zero) && y <= Math.max(point.y, zero);
        const distance = onDot ? dotDistance : onStem ? dx + radius + 3 : Infinity;
        if (distance < nearest) { nearest = distance; hitIndex = datasetIndex; }
      });
      highlighted.current = hitIndex === null ? null : { source: speed, selected, datasetIndex: hitIndex, index: bucket };
      syncActive();
      const model = hitIndex === null ? null : selected[hitIndex];
      setHover((previous) => previous?.source === speed && previous.bucket === bucket && previous.model === model && previous.x === x && previous.y === y && previous.touch === touchInput.current
        ? previous : { source: speed, bucket, model, x, y, touch: touchInput.current });
    },
  }), [models, selected, speed, pickerOpen, closeOnLeave, beforeDatasetsUpdate, isMobile]);
  const liveHandlers = useRef(handlers);
  useLayoutEffect(() => { liveHandlers.current = handlers; }, [handlers]);
  // react-chartjs-2 只在建图时注册局部插件，稳定实例从最新 handlers 读取选择和刷新数据。
  const plugin = useMemo<Plugin<'scatter'>>(() => ({
    id: 'analysisModelSpeed',
    beforeDatasetsUpdate: (chart, args, options) => liveHandlers.current.beforeDatasetsUpdate?.(chart, args, options),
    beforeDatasetsDraw: (chart, args, options) => liveHandlers.current.beforeDatasetsDraw?.(chart, args, options),
    afterEvent: (chart, args, options) => liveHandlers.current.afterEvent?.(chart, args, options),
  }), []);
  const chartOptions = useMemo<ChartOptions<'scatter'>>(() => ({
    responsive: true,
    maintainAspectRatio: false,
    animation: reducedMotion ? false : { duration: MODEL_SPEED_ANIMATION_MS },
    animations: {
      colors: { duration: reducedMotion ? 0 : 180 },
      radius: { duration: reducedMotion ? 0 : 180 },
      borderWidth: { duration: reducedMotion ? 0 : 180 },
    },
    // 初次容器 resize 也保持动画时长，避免默认 resize 的 0ms 覆盖刚开始的入场。
    transitions: { resize: { animation: { duration: reducedMotion ? 0 : MODEL_SPEED_ANIMATION_MS } } },
    layout: { padding: { top: 8, right: 8, bottom: 4 } },
    plugins: { legend: { display: false }, tooltip: { enabled: false } },
    scales: {
      x: { type: 'linear', min: -0.5, max: Math.max(0.5, speed.buckets.length - 0.5),
        grid: { color: theme.grid, drawTicks: false }, border: { color: theme.axis },
        afterBuildTicks: (scale) => { scale.ticks = labels.map((_, index) => ({ value: index })); },
        ticks: { color: theme.textSecondary, font: { size: 10 }, maxRotation: 0, autoSkip: false,
          callback: (value) => labels[Number(value)] ?? '' },
      },
      y: { beginAtZero: true, min: 0, grid: { color: theme.grid }, border: { color: theme.axis },
        title: { display: true, text: 't/s', color: theme.textSecondary, font: { size: 10 } },
        ticks: { color: theme.textSecondary, font: { size: 10 }, maxTicksLimit: 5, callback: (value) => formatCompactNumber(Number(value)) },
      },
    },
  }), [speed.buckets.length, labels, theme, reducedMotion]);
  const visibleHover = !initialLoading && !pickerOpen && hover?.source === speed && selected.length ? hover : null;
  const hoveredBucket = visibleHover ? speed.buckets[visibleHover.bucket] : null;
  const tooltipRows = visibleHover ? modelSpeedTooltipRows(speed, selected, visibleHover.bucket).map((row) => ({
    model: row.model,
    color: colorAt(colors[row.model], isDark),
    detail: row.mean === null ? t('usage_stats.no_data') : `${row.mean.toFixed(1)} t/s`,
    secondary: t('usage_stats.model_speed_samples', { samples: row.count.toLocaleString(locale) }),
    highlighted: row.model === visibleHover.model,
  })) : [];
  const legendItems = selected.map((model) => ({ model, color: colorAt(colors[model], isDark), mean: modelSpeedCombinedMean(models.get(model)?.samples ?? []) }));

  return <section className={`${panelStyles.analysisCard} ${styles.card} keeper-card-surface`} data-analysis-model-speed>
    <div className={`${styles.header} keeper-card-heading`}>
      <div><h2 className="keeper-card-title">{t('usage_stats.analysis_model_speed_title')}</h2>
        <p className="keeper-card-subtitle">{t('usage_stats.analysis_model_speed_subtitle')}</p></div>
      <ModelSpeedPicker candidates={candidates} selected={selected} onChange={choose} onReset={reset} disabled={initialLoading} onOpenChange={changePickerOpen} />
    </div>
    <div className={styles.body}>
      {initialLoading ? <div className={`${panelStyles.emptyState} ${styles.empty}`}>{t('common.loading')}</div>
        : selected.length === 0 ? <div className={`${panelStyles.emptyState} ${styles.empty}`}>{t(candidates.length === 0 ? 'usage_stats.no_data' : 'usage_stats.model_speed_empty_selection')}</div>
        : speed.buckets.length === 0 ? <div className={`${panelStyles.emptyState} ${styles.empty}`}>{t('usage_stats.no_data')}</div>
        : <div className={styles.chart} onPointerDown={(event) => { touchInput.current = event.pointerType === 'touch'; }}
          onPointerMove={(event) => { if (event.pointerType === 'mouse') touchInput.current = false; }}>
          <Scatter data={chartData} options={chartOptions} plugins={[plugin]} />
        </div>}
      <ModelSpeedLegend items={legendItems} highlight={{ ...legendHighlight, active: activeModel }} onInteract={interactWithLegend} />
    </div>
    {visibleHover && hoveredBucket && <ModelSpeedTooltip canvasRef={canvasRef} x={visibleHover.x} y={visibleHover.y} touch={visibleHover.touch} theme={theme}
      title={formatModelSpeedTimeRange(hoveredBucket.start, hoveredBucket.end, timezone, locale, granularity === 'daily' ? 'date' : 'minutes', speed.buckets.flatMap((bucket) => [bucket.start, bucket.end]))}
      rows={tooltipRows} dismiss={dismiss} />}
  </section>;
}
