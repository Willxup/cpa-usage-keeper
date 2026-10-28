import { disambiguateTimeLabels } from '@/utils/usage/timeLabels';
import type { AnalysisModelSpeedPayload, AnalysisModelSpeedSample } from '@/lib/types';

export function modelSpeedMean(sample?: Pick<AnalysisModelSpeedSample, 'speed_tps_sum' | 'speed_sample_count'>): number | null {
  if (!sample || sample.speed_sample_count <= 0) return null;
  const mean = sample.speed_tps_sum / sample.speed_sample_count;
  return Number.isFinite(mean) ? mean : null;
}

export function modelSpeedCombinedMean(samples: readonly (Pick<AnalysisModelSpeedSample, 'speed_tps_sum' | 'speed_sample_count'> | undefined)[]) {
  // 整体均速合并所有有效样本的 sum/count，不能平均各桶或两段的均值。
  const total = { speed_tps_sum: 0, speed_sample_count: 0 };
  for (const sample of samples) {
    total.speed_tps_sum += sample?.speed_tps_sum ?? 0;
    total.speed_sample_count += sample?.speed_sample_count ?? 0;
  }
  return modelSpeedMean(total);
}

// 按服务端真实时间边界合并连续组，始终先加 sum/count；不重新推算自然日长度。
export function mergeModelSpeedBuckets(speed: AnalysisModelSpeedPayload, maximum = 8): AnalysisModelSpeedPayload {
  const count = speed.buckets.length;
  const groups = Math.min(count, maximum);
  if (count <= maximum) return speed;
  const ranges = Array.from({ length: groups }, (_, index) => {
    const start = index * Math.floor(count / groups) + Math.min(index, count % groups);
    return { start, end: start + Math.floor(count / groups) + (index < count % groups ? 1 : 0) };
  });
  return {
    ...speed,
    buckets: ranges.map(({ start, end }) => ({ start: speed.buckets[start].start, end: speed.buckets[end - 1].end })),
    series: speed.series.map((series) => ({
      ...series,
      samples: ranges.map(({ start, end }) => {
        const sample = { speed_tps_sum: 0, speed_sample_count: 0 };
        for (let index = start; index < end; index++) {
          sample.speed_tps_sum += series.samples[index]?.speed_tps_sum ?? 0;
          sample.speed_sample_count += series.samples[index]?.speed_sample_count ?? 0;
        }
        return { ...sample, mean_tps: modelSpeedMean(sample) };
      }),
    })),
  };
}

export function modelSpeedTooltipRows(speed: AnalysisModelSpeedPayload, selected: readonly string[], bucket: number) {
  const models = new Map(speed.series.map((series) => [series.model, series]));
  return selected.map((model) => {
    const sample = models.get(model)?.samples[bucket];
    return { model, mean: modelSpeedMean(sample), count: sample?.speed_sample_count ?? 0 };
  }).sort((left, right) => {
    if (left.mean === null && right.mean !== null) return 1;
    if (right.mean === null && left.mean !== null) return -1;
    return (right.mean ?? 0) - (left.mean ?? 0) || (left.model < right.model ? -1 : left.model > right.model ? 1 : 0);
  });
}

export function modelSpeedBucketIndex(value: number, count: number) {
  return Math.max(0, Math.min(count - 1, Math.round(value)));
}

export function formatModelSpeedTimeRange(start: string | undefined, end: string | undefined, timezone: string, locale: string, precision: 'seconds' | 'minutes' | 'date' = 'seconds', context: readonly string[] = []) {
  if (!start || !end) return '';
  try {
    const startDate = new Date(start);
    const endDate = new Date(end);
    const dayFormat = new Intl.DateTimeFormat(locale, { timeZone: timezone, year: 'numeric', month: '2-digit', day: '2-digit' });
    // 按响应时区判断跨日；小时范围只留时间，跨日补月日，日粒度只显示月日。
    const crossesDay = dayFormat.format(startDate) !== dayFormat.format(endDate);
    const format = new Intl.DateTimeFormat(locale, {
      timeZone: timezone, hourCycle: 'h23',
      ...(precision === 'date'
        ? { month: 'numeric' as const, day: 'numeric' as const }
        : { hour: '2-digit' as const, minute: '2-digit' as const,
          ...(precision === 'seconds' ? { second: '2-digit' as const } : {}),
          ...(crossesDay ? { month: '2-digit' as const, day: '2-digit' as const } : {}) }),
    });
    if (precision === 'date') return `${format.format(startDate)} – ${format.format(endDate)}`;
    const labels = disambiguateTimeLabels([start, end, ...context], timezone, (value) => format.format(new Date(value)), precision);
    return `${labels[0]} – ${labels[1]}`;
  } catch { return `${start} – ${end}`; }
}
