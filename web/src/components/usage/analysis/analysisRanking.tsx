import { useRef, useState, type CSSProperties, type PointerEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { calculateCacheReadRate, formatCompactNumber, formatPerMinuteValue, formatUsd } from '@/utils/usage';
import type { UsageChartGradientColor } from '@/utils/usage/chartConfig';
import styles from './AnalysisPanel.module.scss';

export type AnalysisRankingItem = {
  key: string;
  label: string;
  total: number;
  share: number;
  requests: number;
  cost: number | null;
  inputTokens: number | null;
  cacheReadTokens: number | null;
  color: UsageChartGradientColor;
};

// 优先保留图表原有的固定色序，超出色板的条目再按标识生成稳定的扩展色。
export function getAnalysisRankingColor(identity: string, index: number, palette: readonly UsageChartGradientColor[]): UsageChartGradientColor {
  if (palette[index]) return palette[index];
  let hash = 2166136261;
  for (const character of identity) hash = Math.imul(hash ^ character.codePointAt(0)!, 16777619);
  const hue = (hash >>> 0) % 360;
  const hex = (saturation: number, lightness: number) => {
    const amplitude = saturation * Math.min(lightness, 1 - lightness);
    return `#${[0, 8, 4].map((offset) => {
      const channel = (offset + hue / 30) % 12;
      const value = lightness - amplitude * Math.max(-1, Math.min(channel - 3, 9 - channel, 1));
      return Math.round(value * 255).toString(16).padStart(2, '0');
    }).join('')}`;
  };
  return { base: hex(0.68, 0.44), light: hex(0.78, 0.7) };
}

export function useAnalysisHighlight(scope: string) {
  const [state, setState] = useState({ scope, hovered: null as string | null, focused: null as string | null, selected: null as string | null });
  const pointerFocus = useRef(false);
  const current = state.scope === scope ? state : { scope, hovered: null, focused: null, selected: null };
  const update = (change: Partial<typeof current>) => setState((previous) => ({
    ...(previous.scope === scope ? previous : { scope, hovered: null, focused: null, selected: null }), ...change,
  }));
  return {
    active: current.focused ?? current.hovered ?? current.selected,
    selected: current.selected,
    events: (key: string) => ({
      onPointerEnter: (event: PointerEvent<HTMLButtonElement>) => {
        if (event.pointerType === 'mouse' && window.matchMedia('(hover: hover) and (pointer: fine)').matches) update({ hovered: key });
      },
      onPointerLeave: () => update({ hovered: null }),
      // 点按独立控制选中状态，避免触摸产生的 focus 让第二次点按无法取消高亮。
      onPointerDown: () => { pointerFocus.current = true; },
      onPointerUp: () => { pointerFocus.current = false; },
      onPointerCancel: () => { pointerFocus.current = false; },
      onFocus: () => { if (!pointerFocus.current) update({ focused: key }); },
      onBlur: () => { pointerFocus.current = false; update({ focused: null }); },
      onClick: () => update({ selected: current.selected === key ? null : key, focused: null, hovered: null }),
    }),
  };
}

export function AnalysisRankingList({ items, label, windowMinutes, highlight }: {
  items: AnalysisRankingItem[];
  label: string;
  windowMinutes: number | null;
  highlight: ReturnType<typeof useAnalysisHighlight>;
}) {
  const { t } = useTranslation();
  const active = items.some((item) => item.key === highlight.active) ? highlight.active : null;
  const rate = (value: number) => windowMinutes && windowMinutes > 0 ? formatPerMinuteValue(value / windowMinutes) : '--';
  return (
    <ol className={styles.rankingList} aria-label={label}>
      {items.map((item, index) => {
        const cacheRate = item.inputTokens === null || item.cacheReadTokens === null ? null : calculateCacheReadRate({ inputTokens: item.inputTokens, cacheReadTokens: item.cacheReadTokens });
        return (
          <li key={item.key}>
            <button type="button" className={styles.rankingItem}
              style={{ '--ranking-color': item.color.base } as CSSProperties}
              data-active={active === item.key} data-muted={Boolean(active && active !== item.key)}
              aria-pressed={highlight.selected === item.key}
              aria-label={`${index + 1}. ${item.label}, ${t('usage_stats.total_tokens')}: ${formatCompactNumber(item.total)}, ${t('usage_stats.analysis_composition_token_percent')}: ${item.share.toFixed(2)}%, ${t('usage_stats.cache_rate')}: ${cacheRate === null ? '--' : `${cacheRate.toFixed(2)}%`}`}
              {...highlight.events(item.key)}
            >
              <span className={styles.rankingTopline}>
                <span className={styles.rankingRank}>{index + 1}</span>
                <span className={styles.rankingColor} aria-hidden="true" />
                <span className={styles.rankingName} title={item.label}>{item.label}</span>
                <strong className={styles.rankingTokens}>{formatCompactNumber(item.total)}</strong>
                <span className={styles.rankingShare}>{item.share.toFixed(2)}%</span>
              </span>
              <span className={styles.rankingTrack} aria-hidden="true"><span style={{ width: `${Math.max(0, Math.min(100, item.share))}%` }} /></span>
              <span className={styles.rankingMetrics}>
                <span data-metric="cache" className={styles.rankingCache}>{t('usage_stats.cache_rate')} <strong>{cacheRate === null ? '--' : `${cacheRate.toFixed(2)}%`}</strong></span>
                <span>{t('usage_stats.requests_count')} <strong>{formatCompactNumber(item.requests)}</strong></span>
                <span>{t('usage_stats.total_cost')} <strong>{item.cost === null ? '--' : formatUsd(item.cost)}</strong></span>
                <span>{t('usage_stats.rpm')} <strong>{rate(item.requests)}</strong></span>
                <span>{t('usage_stats.tpm')} <strong>{rate(item.total)}</strong></span>
              </span>
            </button>
          </li>
        );
      })}
    </ol>
  );
}
