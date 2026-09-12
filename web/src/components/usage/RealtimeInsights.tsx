import { useMemo, type CSSProperties } from 'react';
import { useTranslation } from 'react-i18next';
import '@/lib/chartjs';
import { Chart, Doughnut } from 'react-chartjs-2';
import { Chart as ChartJS, type ChartData, type ChartOptions, type Plugin } from 'chart.js';
import type { RealtimeCacheLevelPoint, RealtimeInsights, RealtimeWindowSummary } from '@/lib/types';
import { Card } from '@/components/ui/Card';
import { formatCompactNumber, formatUsd } from '@/utils/usage';
import { buildUsageChartTooltipStyle, getUsageChartTheme, toUsageChartGradientFill, USAGE_CHART_TOKEN_COLORS as COLORS } from '@/utils/usage/chartConfig';
import usageStyles from '@/pages/UsagePage.module.scss';
import styles from './RealtimeInsights.module.scss';

const FAILURE_COLOR = { base: '#b91c1c', light: '#fca5a5' };
const CACHE_RATE_COLOR = '#14b8a6';
const FAILURE_RATE_COLOR = '#f97316';
const ratio = (value: number, denominator: number) => denominator > 0 ? value / denominator * 100 : null;
const percent = (value: number | null) => value === null ? '—' : `${value.toFixed(1)}%`;
type Translate = (key: string) => string;

interface CacheShareCenterOptions {
  value: string;
  label: string;
  primary: string;
  secondary: string;
}
type TokenMixOptions = ChartOptions<'doughnut'> & {
  plugins?: ChartOptions<'doughnut'>['plugins'] & {realtimeCacheShareCenter?: CacheShareCenterOptions};
};
// 中心文字在原生 Tooltip 之前画入同一画布；稳定插件从每次更新的 options 读取数据，避免闭包过期。
const cacheShareCenterPlugin: Plugin<'doughnut', CacheShareCenterOptions> = {
  id: 'realtimeCacheShareCenter',
  afterDatasetsDraw(chart, _args, options) {
    const {ctx,chartArea} = chart;
    const x=(chartArea.left+chartArea.right)/2, y=(chartArea.top+chartArea.bottom)/2;
    ctx.save();
    ctx.textAlign='center';
    ctx.textBaseline='middle';
    ctx.fillStyle=options.primary;
    ctx.font=`600 ${chart.width < 190 ? 18 : 20}px ${ChartJS.defaults.font.family}`;
    ctx.fillText(options.value,x,y-7,chart.width*.58);
    ctx.fillStyle=options.secondary;
    ctx.font=`11px ${ChartJS.defaults.font.family}`;
    ctx.fillText(options.label,x,y+15,chart.width*.58);
    ctx.restore();
  },
};
const TOKEN_MIX_PLUGINS = [cacheShareCenterPlugin];

export function buildRealtimeCacheData(points: RealtimeCacheLevelPoint[], labels: string[], t: Translate): ChartData<'bar' | 'line', Array<number | null>, string> {
  return { labels, datasets: [
    { type: 'bar', label: t('usage_stats.insights_uncached'), data: points.map(p => Math.max(0, p.input_tokens - p.cache_read_tokens - p.cache_creation_tokens)), backgroundColor: context => toUsageChartGradientFill(context, COLORS.input), borderColor: COLORS.input.base, stack: 'input', order: 2 },
    { type: 'bar', label: t('usage_stats.comparison_cache_read'), data: points.map(p => p.cache_read_tokens), backgroundColor: context => toUsageChartGradientFill(context, COLORS.cacheRead), borderColor: COLORS.cacheRead.base, stack: 'input', order: 2 },
    { type: 'bar', label: t('usage_stats.comparison_cache_write'), data: points.map(p => p.cache_creation_tokens), backgroundColor: context => toUsageChartGradientFill(context, COLORS.cacheWrite), borderColor: COLORS.cacheWrite.base, stack: 'input', order: 2 },
    { type: 'line', label: t('usage_stats.comparison_cache'), data: points.map(p => p.cache_read_rate ?? null), borderColor: CACHE_RATE_COLOR, backgroundColor: CACHE_RATE_COLOR, yAxisID: 'rate', borderWidth: 2, pointRadius: 0, pointHoverRadius: 4, tension: .3, order: 1 },
  ] };
}

function mixedOptions(isDark: boolean, isMobile: boolean, axisTitle: string, rateTitle: string, rateMax?: number): ChartOptions<'bar' | 'line'> {
  const theme = getUsageChartTheme(isDark);
  return {
    responsive: true, maintainAspectRatio: false, animation: false,
    interaction: { mode: 'index', intersect: false },
    datasets: { bar: { borderWidth: 0, borderRadius: 2, borderSkipped: false, barPercentage: .82, categoryPercentage: .78, maxBarThickness: 38 } },
    plugins: {
      legend: { position: 'bottom', labels: { color: theme.textSecondary, usePointStyle: true, pointStyle: 'rectRounded', boxWidth: 8, boxHeight: 8, padding: 14, font: { size: isMobile ? 10 : 11 } } },
      tooltip: {
        ...buildUsageChartTooltipStyle(theme),
        callbacks: { label: ctx => `${ctx.dataset.label}: ${ctx.dataset.yAxisID === 'rate' ? percent(ctx.parsed.y) : formatCompactNumber(ctx.parsed.y ?? 0)}` },
      },
    },
    scales: {
      x: { stacked: true, grid: { color: theme.grid, drawTicks: false }, border: { color: theme.axis }, ticks: { color: theme.textSecondary, maxTicksLimit: isMobile ? 4 : 7, maxRotation: 0, font: { size: 10 } } },
      y: { stacked: true, beginAtZero: true, grid: { color: theme.grid }, border: { color: theme.axis }, title: { display: !isMobile, text: axisTitle, color: theme.textSecondary, font: { size: 10 } }, ticks: { color: theme.textSecondary, maxTicksLimit: 5, callback: v => formatCompactNumber(Number(v)), font: { size: 10 } } },
      rate: { position: 'right', beginAtZero: true, max: rateMax, grid: { drawOnChartArea: false }, border: { color: theme.axis }, title: { display: !isMobile, text: rateTitle, color: theme.textSecondary, font: { size: 10 } }, ticks: { color: theme.textSecondary, maxTicksLimit: 5, callback: v => `${v}%`, font: { size: 10 } } },
    },
  };
}

export function RealtimeWindowCards({ summary: s, window }: { summary: RealtimeWindowSummary; window: string }) {
  const { t } = useTranslation();
  const cards = [
    { label: 'requests', value: s.requests.toLocaleString(), color: COLORS.input.base },
    { label: 'tokens', value: formatCompactNumber(s.total_tokens), color: COLORS.reasoning.base },
    { label: 'cache_reach', value: percent(ratio(s.cached_requests, s.token_requests)), color: COLORS.cacheRead.base },
    { label: 'cost', value: s.cost === null ? '—' : formatUsd(s.cost), color: COLORS.output.base },
  ];
  return <div className={styles.summaryGrid} data-realtime-summary>
    {cards.map(card => <div key={card.label} className={`${usageStyles.statCard} ${styles.summaryCard}`} style={{'--accent':card.color, '--accent-soft':`${card.color}18`, '--accent-border':`${card.color}55`} as CSSProperties}>
      <div className={styles.summaryLabel}><span>{t(`usage_stats.insights_${card.label}`)}</span><span className={styles.window}>{window}</span></div>
      <strong>{card.value}</strong>
    </div>)}
  </div>;
}

export function RealtimeCacheChart({ points, labels, isDark, isMobile }: { points: RealtimeCacheLevelPoint[]; labels: string[]; isDark: boolean; isMobile: boolean }) {
  const { t } = useTranslation();
  const data = useMemo(() => buildRealtimeCacheData(points, labels, t), [points, labels, t]);
  const options = useMemo(() => mixedOptions(isDark, isMobile, t('usage_stats.insights_rolling_input'), t('usage_stats.comparison_cache'), 100), [isDark, isMobile, t]);
  return <div className={`${styles.chartSurface} ${styles.cacheChart}`}><Chart type="bar" data={data} options={options} role="img" aria-label={t('usage_stats.insights_cache_chart')} /></div>;
}

export function RealtimeDiagnostics({ insights, labels, isDark, isMobile }: { insights: RealtimeInsights; labels: string[]; isDark: boolean; isMobile: boolean }) {
  const { t } = useTranslation();
  const { summary: s, outcomes } = insights;
  const outcomeData = useMemo<ChartData<'bar' | 'line', Array<number | null>, string>>(() => ({ labels, datasets: [
    { type: 'bar', label: t('usage_stats.insights_successful'), data: outcomes.map(p => p.requests - p.failures), backgroundColor: context => toUsageChartGradientFill(context, COLORS.output), borderColor: COLORS.output.base, stack: 'requests', order: 2 },
    { type: 'bar', label: t('usage_stats.comparison_failures'), data: outcomes.map(p => p.failures), backgroundColor: context => toUsageChartGradientFill(context, FAILURE_COLOR), borderColor: FAILURE_COLOR.base, stack: 'requests', order: 2 },
    { type: 'line', label: t('usage_stats.comparison_failure_rate'), data: outcomes.map(p => ratio(p.failures, p.requests)), yAxisID: 'rate', borderColor: FAILURE_RATE_COLOR, backgroundColor: FAILURE_RATE_COLOR, pointRadius: 0, pointHoverRadius: 4, borderWidth: 2, borderDash: [5, 4], tension: .3, order: 1 },
  ] }), [outcomes, labels, t]);
  const outcomeOptions = useMemo(() => mixedOptions(isDark, isMobile, t('usage_stats.insights_requests_per_bucket'), t('usage_stats.comparison_failure_rate')), [isDark, isMobile, t]);
  // Reasoning 可能已包含在 Output；保留独立数值，不重复堆叠进构成图。
  const mix = useMemo(() => [
    { label: t('usage_stats.insights_uncached'), value: Math.max(0, s.input_tokens - s.cache_read_tokens - s.cache_creation_tokens), color: COLORS.input },
    { label: t('usage_stats.comparison_cache_read'), value: s.cache_read_tokens, color: COLORS.cacheRead },
    { label: t('usage_stats.comparison_cache_write'), value: s.cache_creation_tokens, color: COLORS.cacheWrite },
    { label: t('usage_stats.comparison_output'), value: s.output_tokens, color: COLORS.output },
  ], [s.input_tokens, s.cache_read_tokens, s.cache_creation_tokens, s.output_tokens, t]);
  const mixTotal = mix.reduce((sum, item) => sum + item.value, 0);
  const mixData = useMemo<ChartData<'doughnut', number[], string>>(() => ({ labels: mix.map(item => item.label), datasets: [{ data: mix.map(item => item.value), backgroundColor: context => toUsageChartGradientFill(context, mix[context.dataIndex].color), borderWidth: 0, borderRadius: 8, hoverOffset: 6 }] }), [mix]);
  const cacheShare = percent(ratio(s.cache_read_tokens,mixTotal));
  const cacheShareLabel = t('usage_stats.insights_cache_share');
  const mixOptions = useMemo<TokenMixOptions>(() => {
    const theme=getUsageChartTheme(isDark);
    return {
      responsive:true, maintainAspectRatio:false, animation:false, cutout:'64%', spacing:3, layout:{padding:8},
      plugins:{
        legend:{display:false},
        realtimeCacheShareCenter:{value:cacheShare,label:cacheShareLabel,primary:theme.textPrimary,secondary:theme.textSecondary},
        tooltip:{
          ...buildUsageChartTooltipStyle(theme),
          callbacks:{
            label:ctx => `${formatCompactNumber(ctx.parsed)} · ${percent(ratio(ctx.parsed,mixTotal))}`,
          },
        },
      },
    };
  },[cacheShare,cacheShareLabel,isDark,mixTotal]);
  return <div className={styles.diagnosticsGrid} data-realtime-diagnostics>
    <Card title={t('usage_stats.insights_outcomes')}>
      <div className={styles.chartSurface}><div className={styles.chart}><Chart type="bar" data={outcomeData} options={outcomeOptions} role="img" aria-label={t('usage_stats.insights_outcomes')} /></div></div>
      <div className={styles.outcomeSummary}><span>{t('usage_stats.insights_successful')} <strong>{(s.requests - s.failures).toLocaleString()}</strong></span><span>{t('usage_stats.comparison_failures')} <strong>{s.failures.toLocaleString()}</strong></span><span>{t('usage_stats.comparison_failure_rate')} <strong>{percent(ratio(s.failures, s.requests))}</strong></span></div>
    </Card>
    <Card title={t('usage_stats.insights_mix')}>
      <div className={`${styles.chartSurface} ${styles.mix}`}>
        <div className={styles.doughnut}>
          {mixTotal > 0 ? <Doughnut data={mixData} options={mixOptions} plugins={TOKEN_MIX_PLUGINS} role="img" aria-label={`${t('usage_stats.insights_mix')}: ${cacheShareLabel} ${cacheShare}`} /> : <div className={styles.emptyRing}><span>—</span></div>}
        </div>
        <dl className={styles.mixLegend}>{mix.map(item => <div key={item.label} className={styles.mixItem}>
          <dt><i style={{background:`linear-gradient(180deg, ${item.color.light}, ${item.color.base})`}} />{item.label}</dt>
          <dd><strong>{formatCompactNumber(item.value)}</strong><span>{percent(ratio(item.value,mixTotal))}</span></dd>
          <dd className={styles.mixTrack} aria-hidden="true">
            <i style={{width:`${ratio(item.value,mixTotal) ?? 0}%`,background:`linear-gradient(90deg, ${item.color.light}, ${item.color.base})`}} />
          </dd>
        </div>)}</dl>
      </div>
      <div className={styles.outcomeSummary}><span>{t('usage_stats.comparison_reasoning')} <strong>{formatCompactNumber(s.reasoning_tokens)}</strong></span><span>{t('usage_stats.insights_token_requests')} <strong>{s.token_requests.toLocaleString()}</strong></span></div>
    </Card>
  </div>;
}
