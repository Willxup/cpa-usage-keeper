import { useId, useLayoutEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { RealtimeUsageTopItem } from '@/lib/types';
import { calculateCacheReadRate, formatCompactNumber, formatFixedTwoDecimals, formatUsd } from '@/utils/usage';
import { USAGE_CHART_COMPOSITION_COLORS } from '@/utils/usage/chartConfig';
import styles from '@/pages/UsagePage.module.scss';

const OTHER_COLOR = { base: '#94a3b8', light: '#cbd5e1' };
const LIGHT_SHARE_TEXT_COLORS = ['#235ee7', '#0f766e', '#7c3aed', '#a84d08', '#0e7490', '#be123c'] as const;
const OTHER_LIGHT_SHARE_TEXT_COLOR = '#5b697c';
const OTHER_KEY = '__realtime_others__';

interface RibbonGeometry {
  width: number;
  height: number;
  rows: { y: number; height: number }[];
}

function sameGeometry(left: RibbonGeometry | null, right: RibbonGeometry): boolean {
  return left !== null && left.width === right.width && left.height === right.height &&
    left.rows.length === right.rows.length && left.rows.every((row, index) =>
      row.y === right.rows[index].y && row.height === right.rows[index].height);
}

export function RealtimeTokenShareRibbons({ items, loading, isDark = false }: {
  items: readonly RealtimeUsageTopItem[];
  loading: boolean;
  isDark?: boolean;
}) {
  const { t } = useTranslation();
  const gradientId = useId();
  const containerRef = useRef<HTMLDivElement>(null);
  const [geometry, setGeometry] = useState<RibbonGeometry | null>(null);

  useLayoutEffect(() => {
    const container = containerRef.current;
    if (!container || items.length === 0) return;
    const canvas = container.querySelector<HTMLElement>('[data-ribbon-canvas-wrap]');
    const rows = Array.from(container.querySelectorAll<HTMLElement>('[data-ribbon-row]'));
    if (!canvas) return;
    let frame = 0;
    const measure = () => {
      frame = 0;
      const origin = container.getBoundingClientRect();
      const next: RibbonGeometry = {
        width: canvas.getBoundingClientRect().width,
        height: origin.height,
        rows: rows.map((row) => {
          const rect = row.getBoundingClientRect();
          return { y: rect.top - origin.top, height: rect.height };
        }),
      };
      setGeometry((previous) => sameGeometry(previous, next) ? previous : next);
    };
    measure();
    // 绝对定位的 SVG 不参与排版；下一帧测量避免 ResizeObserver 的同步回写循环。
    const observer = new ResizeObserver(() => {
      if (!frame) frame = requestAnimationFrame(measure);
    });
    observer.observe(container);
    observer.observe(canvas);
    rows.forEach((row) => observer.observe(row));
    return () => {
      observer.disconnect();
      if (frame) cancelAnimationFrame(frame);
    };
  }, [items]);

  if (items.length === 0) {
    return <div className={styles.overviewRealtimeEmpty} aria-busy={loading}>{t('usage_stats.overview_realtime_usage_empty')}</div>;
  }

  const ribbons = items.map((item, index) => {
    const isOther = index === 5 && items.length === 6 && item.key === OTHER_KEY;
    const paletteIndex = index % USAGE_CHART_COMPOSITION_COLORS.length;
    const color = isOther ? OTHER_COLOR : USAGE_CHART_COMPOSITION_COLORS[paletteIndex];
    const share = Number.isFinite(item.share) ? Math.max(0, Math.min(100, item.share)) : 0;
    const accent = isDark ? color.light : color.base;
    const shareColor = isDark ? color.light : (isOther ? OTHER_LIGHT_SHARE_TEXT_COLOR : LIGHT_SHARE_TEXT_COLORS[paletteIndex]);
    return { item, isOther, share, color, accent, shareColor };
  });
  // 源条和每个目标端共用一个缩放比例，最大份额也只能落在自己那一行内。
  const measured = geometry?.rows.length === items.length ? geometry : null;
  const sourceHeight = measured ? Math.max(0, Math.min(
    measured.width * 1.1,
    Math.max(0, measured.height - 32) * 0.7,
    ...ribbons.map(({ share }, index) => share > 0 ? measured.rows[index].height * 0.78 * 100 / share : Infinity),
  )) : 0;
  const sourceY = measured ? (measured.height - sourceHeight) / 2 : 0;
  const sourceOffsets = ribbons.map((_, index) => ribbons.slice(0, index).reduce((sum, { share }) => sum + share, 0));

  return (
    <div className={styles.overviewRealtimeRibbon} data-realtime-ribbon aria-busy={loading} ref={containerRef}>
      <div className={styles.overviewRealtimeRibbonCanvasWrap} data-ribbon-canvas-wrap>
        {measured && measured.width > 0 && measured.height > 0 && (
          <svg className={styles.overviewRealtimeRibbonCanvas} data-ribbon-canvas
            viewBox={`0 0 ${measured.width} ${measured.height}`} preserveAspectRatio="none" aria-hidden="true">
            <defs>
              {ribbons.map(({ item, color }, index) => (
                <linearGradient key={`${index}:${item.key}`} id={`${gradientId}-${index}`} gradientUnits="userSpaceOnUse" x1="11" y1="0" x2={measured.width - 3} y2="0">
                  <stop offset="0%" stopColor={color.base} stopOpacity={isDark ? 0.44 : 0.38} />
                  <stop offset="100%" stopColor={color.light} stopOpacity={isDark ? 0.26 : 0.22} />
                </linearGradient>
              ))}
            </defs>
            <text x="3" y={Math.max(13, sourceY - 8)} className={styles.overviewRealtimeRibbonTotal}>100%</text>
            {ribbons.map(({ item, share, accent }, index) => {
              const thickness = sourceHeight * share / 100;
              const startY = sourceY + sourceHeight * sourceOffsets[index] / 100;
              if (thickness <= 0) return null;
              const row = measured.rows[index];
              const endY = row.y + (row.height - thickness) / 2;
              const endX = measured.width - 3;
              const curveA = measured.width * 0.43;
              const curveB = measured.width * 0.57;
              const id = `${gradientId}-${index}`;
              return (
                <g key={`${index}:${item.key}`} data-ribbon-path={index} data-ribbon-share={share}>
                  <path d={`M 11 ${startY} C ${curveA} ${startY}, ${curveB} ${endY}, ${endX} ${endY} L ${endX} ${endY + thickness} C ${curveB} ${endY + thickness}, ${curveA} ${startY + thickness}, 11 ${startY + thickness} Z`}
                    fill={`url(#${id})`} />
                  <rect x="3" y={startY} width="8" height={thickness} fill={accent} />
                  <rect x={endX} y={endY} width="3" height={thickness} fill={accent} />
                </g>
              );
            })}
          </svg>
        )}
      </div>
      <div className={styles.overviewRealtimeRibbonRows}>
        {ribbons.map(({ item, isOther, share, accent, shareColor }, index) => {
          const cacheRate = item.input_tokens == null || item.cache_read_tokens == null ? null : calculateCacheReadRate({
            inputTokens: item.input_tokens, cacheReadTokens: item.cache_read_tokens,
          });
          return (
            <div key={`${index}:${item.key}`} className={styles.overviewRealtimeRibbonRow} data-ribbon-row={index}>
              <div className={styles.overviewRealtimeRibbonTopline}>
                <span className={styles.overviewRealtimeRibbonDot} style={{ backgroundColor: accent }} aria-hidden="true" />
                <span className={styles.overviewRealtimeRibbonName}>{isOther ? t('usage_stats.comparison_others') : item.label}</span>
                <strong className={styles.overviewRealtimeRibbonShare} style={{ color: shareColor }}>{formatFixedTwoDecimals(share)}%</strong>
              </div>
              <div className={styles.overviewRealtimeRibbonMetrics}>
                <span>{t('usage_stats.overview_realtime_tokens_label')} <strong>{formatCompactNumber(item.tokens)}</strong></span>
                <span data-metric="cache" className={styles.overviewRealtimeRibbonCache}>{t('usage_stats.overview_realtime_cache_rate')} <strong>{cacheRate === null ? '—' : `${formatFixedTwoDecimals(cacheRate)}%`}</strong></span>
                <span>{t('usage_stats.overview_realtime_cost_label')} <strong>{item.cost == null ? '—' : formatUsd(item.cost)}</strong></span>
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
