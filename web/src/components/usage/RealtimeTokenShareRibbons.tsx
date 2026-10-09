import { useLayoutEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { RealtimeUsageTopItem } from '@/lib/types';
import { formatCompactNumber, formatFixedTwoDecimals, formatUsd } from '@/utils/usage';
import styles from '@/pages/UsagePage.module.scss';

const COLORS = ['#4d8bdc', '#269c85', '#9671ce', '#d99637', '#d1698c', '#a5b0c0'];
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

function MetaPill({ label, value }: { label: string; value: string }) {
  return (
    <span className={styles.overviewRealtimeUsageMetaPill}>
      <span className={styles.overviewRealtimeUsageMetaLabel}>{label}</span>
      <span className={styles.overviewRealtimeUsageMetaValue}>{value}</span>
    </span>
  );
}

export function RealtimeTokenShareRibbons({ items, loading }: { items: readonly RealtimeUsageTopItem[]; loading: boolean }) {
  const { t } = useTranslation();
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

  const shares = items.map((item) => Number.isFinite(item.share) ? Math.max(0, Math.min(100, item.share)) : 0);
  // 源条和每个目标端共用一个缩放比例，最大份额也只能落在自己那一行内。
  const measured = geometry?.rows.length === items.length ? geometry : null;
  const sourceHeight = measured ? Math.max(0, Math.min(
    measured.width * 1.1,
    Math.max(0, measured.height - 32) * 0.7,
    ...shares.map((share, index) => share > 0 ? measured.rows[index].height * 0.78 * 100 / share : Infinity),
  )) : 0;
  const sourceY = measured ? (measured.height - sourceHeight) / 2 : 0;
  const sourceOffsets = shares.map((_, index) => shares.slice(0, index).reduce((sum, share) => sum + share, 0));

  return (
    <div className={styles.overviewRealtimeRibbon} data-realtime-ribbon aria-busy={loading} ref={containerRef}>
      <div className={styles.overviewRealtimeRibbonCanvasWrap} data-ribbon-canvas-wrap>
        {measured && measured.width > 0 && measured.height > 0 && (
          <svg className={styles.overviewRealtimeRibbonCanvas} data-ribbon-canvas
            viewBox={`0 0 ${measured.width} ${measured.height}`} preserveAspectRatio="none" aria-hidden="true">
            <text x="3" y={Math.max(13, sourceY - 8)} className={styles.overviewRealtimeRibbonTotal}>100%</text>
            {items.map((item, index) => {
              const thickness = sourceHeight * shares[index] / 100;
              const startY = sourceY + sourceHeight * sourceOffsets[index] / 100;
              if (thickness <= 0) return null;
              const row = measured.rows[index];
              const endY = row.y + (row.height - thickness) / 2;
              const endX = measured.width - 3;
              const curveA = measured.width * 0.43;
              const curveB = measured.width * 0.57;
              const color = COLORS[index % COLORS.length];
              return (
                <g key={`${index}:${item.key}`} data-ribbon-path={index} data-ribbon-share={shares[index]}>
                  <path d={`M 11 ${startY} C ${curveA} ${startY}, ${curveB} ${endY}, ${endX} ${endY} L ${endX} ${endY + thickness} C ${curveB} ${endY + thickness}, ${curveA} ${startY + thickness}, 11 ${startY + thickness} Z`}
                    fill={color} className={styles.overviewRealtimeRibbonFlow} />
                  <rect x="3" y={startY} width="8" height={thickness} fill={color} />
                  <rect x={endX} y={endY} width="3" height={thickness} fill={color} />
                </g>
              );
            })}
          </svg>
        )}
      </div>
      <div className={styles.overviewRealtimeRibbonRows}>
        {items.map((item, index) => {
          const isOther = index === 5 && items.length === 6 && item.key === OTHER_KEY;
          return (
            <div key={`${index}:${item.key}`} className={styles.overviewRealtimeRibbonRow} data-ribbon-row={index}>
              <div className={styles.overviewRealtimeRibbonTopline}>
                <span className={styles.overviewRealtimeRibbonDot} style={{ backgroundColor: COLORS[index % COLORS.length] }} aria-hidden="true" />
                <span className={styles.overviewRealtimeRibbonName}>{isOther ? t('usage_stats.comparison_others') : item.label}</span>
                <strong className={styles.overviewRealtimeRibbonShare}>{formatFixedTwoDecimals(shares[index])}%</strong>
              </div>
              <div className={`${styles.overviewRealtimeUsageMeta} ${styles.overviewRealtimeRibbonMeta}`}>
                <MetaPill label={t('usage_stats.overview_realtime_tokens_label')} value={formatCompactNumber(item.tokens)} />
                <MetaPill label={t('usage_stats.overview_realtime_requests_label')} value={item.requests.toLocaleString()} />
                <MetaPill label={t('usage_stats.overview_realtime_cost_label')} value={item.cost == null ? '—' : formatUsd(item.cost)} />
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
