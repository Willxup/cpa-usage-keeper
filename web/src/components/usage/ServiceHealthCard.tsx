import { useState, useCallback, useRef, useEffect, useMemo } from 'react';
import { createPortal } from 'react-dom';
import { useTranslation } from 'react-i18next';
import type { ServiceHealthData, StatusBlockDetail } from '@/utils/usage';
import type { UsageActivityResponse } from '@/lib/types';
import styles from '@/pages/UsagePage.module.scss';

const COLOR_STOPS = [
  { r: 239, g: 68, b: 68 }, // #ef4444
  { r: 250, g: 204, b: 21 }, // #facc15
  { r: 34, g: 197, b: 94 }, // #22c55e
] as const;

const TOOLTIP_OFFSET = 8;
const TOOLTIP_SAFE_WIDTH = 180;
const TOOLTIP_SAFE_HEIGHT = 72;
const ACTIVITY_GRID_ROWS = 7;
const ACTIVITY_GRID_COLUMNS = 52;

type TooltipHorizontalPosition = 'center' | 'left' | 'right';
type TooltipVerticalPosition = 'above' | 'below';

interface ActiveTooltipState {
  idx: number;
  requestIdentity: string;
  anchorEl: HTMLDivElement;
  horizontal: TooltipHorizontalPosition;
  vertical: TooltipVerticalPosition;
  left: number;
  top: number;
  transform: string;
}

function rateToColor(rate: number): string {
  const t = Math.max(0, Math.min(1, rate));
  const segment = t < 0.5 ? 0 : 1;
  const localT = segment === 0 ? t * 2 : (t - 0.5) * 2;
  const from = COLOR_STOPS[segment];
  const to = COLOR_STOPS[segment + 1];
  const r = Math.round(from.r + (to.r - from.r) * localT);
  const g = Math.round(from.g + (to.g - from.g) * localT);
  const b = Math.round(from.b + (to.b - from.b) * localT);
  return `rgb(${r}, ${g}, ${b})`;
}

function createDateTimeFormatter(timeZone?: string): Intl.DateTimeFormat {
  const options: Intl.DateTimeFormatOptions = {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hourCycle: 'h23',
    timeZone,
  };
  try {
    return new Intl.DateTimeFormat('en-US', options);
  } catch {
    return new Intl.DateTimeFormat('en-US', { ...options, timeZone: undefined });
  }
}

function formatDateTime(timestamp: number, formatter: Intl.DateTimeFormat): string {
  const parts = formatter.formatToParts(new Date(timestamp));
  const values = Object.fromEntries(parts.map((part) => [part.type, part.value]));
  return `${values.month}/${values.day} ${values.hour}:${values.minute}`;
}

export function parseTime(value?: string): number {
  if (!value) return 0;
  const nanosecondBoundary = value.match(/^(.*T\d{2}:\d{2}:\d{2})\.9{4,}(Z|[+-]\d{2}:\d{2})$/);
  if (nanosecondBoundary) {
    const rounded = Date.parse(`${nanosecondBoundary[1]}${nanosecondBoundary[2]}`) + 1000;
    return Number.isFinite(rounded) ? rounded : 0;
  }
  const parsed = Date.parse(value);
  return Number.isFinite(parsed) ? parsed : 0;
}

function ServiceHealthTitle({ title, subtitle }: { title: string; subtitle: string }) {
  return (
    <div className={styles.sectionTitleBlock}>
      <h3 className={styles.sectionTitle}>{title}</h3>
      <p className={styles.sectionSubtitle}>{subtitle}</p>
    </div>
  );
}

export interface ServiceHealthCardProps {
  activity: UsageActivityResponse | null;
  loading: boolean;
  requestIdentity: string;
}

export function ServiceHealthCard({ activity, loading, requestIdentity }: ServiceHealthCardProps) {
  const { t } = useTranslation();
  const [activeTooltip, setActiveTooltip] = useState<ActiveTooltipState | null>(null);
  const gridRef = useRef<HTMLDivElement>(null);
  const projectTimeZone = activity?.timezone?.trim() || undefined;
  const dateTimeFormatter = useMemo(() => createDateTimeFormatter(projectTimeZone), [projectTimeZone]);

  const healthData: ServiceHealthData = useMemo(() => {
    const blockDetails = (activity?.blocks ?? []).map((block) => ({
      startTime: Date.parse(block.start_time),
      endTime: Date.parse(block.end_time),
      success: Number(block.success ?? 0),
      failure: Number(block.failure ?? 0),
      rate: Number(block.rate ?? -1),
    }));
    return {
      totalSuccess: Number(activity?.total_success ?? 0),
      totalFailure: Number(activity?.total_failure ?? 0),
      successRate: Number(activity?.success_rate ?? 0),
      rows: ACTIVITY_GRID_ROWS,
      columns: ACTIVITY_GRID_COLUMNS,
      bucketSeconds: Number(activity?.bucket_seconds ?? 0),
      windowStart: parseTime(activity?.window_start),
      windowEnd: parseTime(activity?.window_end),
      blockDetails,
    };
  }, [activity]);

  const hasData = healthData.totalSuccess + healthData.totalFailure > 0;
  // 请求 identity 改变时在当前 render 内清空 tooltip，防止 A→B→A 后恢复旧状态。
  if (activeTooltip && activeTooltip.requestIdentity !== requestIdentity) {
    setActiveTooltip(null);
  }
  const visibleTooltip = activeTooltip?.requestIdentity === requestIdentity ? activeTooltip : null;

  useEffect(() => {
    if (visibleTooltip === null) return;
    const handler = (e: PointerEvent) => {
      if (gridRef.current && !gridRef.current.contains(e.target as Node)) {
        setActiveTooltip(null);
      }
    };
    document.addEventListener('pointerdown', handler);
    return () => document.removeEventListener('pointerdown', handler);
  }, [visibleTooltip]);

  const buildTooltipState = useCallback(
    (idx: number, anchorEl: HTMLDivElement | null, tooltipRequestIdentity: string): ActiveTooltipState | null => {
      if (!anchorEl || !anchorEl.isConnected) {
        return null;
      }

      const rect = anchorEl.getBoundingClientRect();
      const centerX = rect.left + rect.width / 2;

      let horizontal: TooltipHorizontalPosition = 'center';
      let left = centerX;

      if (centerX <= TOOLTIP_SAFE_WIDTH / 2) {
        horizontal = 'left';
        left = rect.left;
      } else if (centerX >= window.innerWidth - TOOLTIP_SAFE_WIDTH / 2) {
        horizontal = 'right';
        left = rect.right;
      }

      const vertical: TooltipVerticalPosition = rect.top <= TOOLTIP_SAFE_HEIGHT ? 'below' : 'above';
      const top = vertical === 'below' ? rect.bottom + TOOLTIP_OFFSET : rect.top - TOOLTIP_OFFSET;
      const translateX = horizontal === 'center' ? '-50%' : horizontal === 'right' ? '-100%' : '0';
      const translateY = vertical === 'below' ? '0' : '-100%';

      return {
        idx,
        requestIdentity: tooltipRequestIdentity,
        anchorEl,
        horizontal,
        vertical,
        left: Math.round(left),
        top: Math.round(top),
        transform: `translate(${translateX}, ${translateY})`,
      };
    },
    []
  );

  useEffect(() => {
    if (!visibleTooltip) return;

    const updateTooltipPosition = () => {
      if (!document.body.contains(visibleTooltip.anchorEl)) {
        setActiveTooltip(null);
        return;
      }
      setActiveTooltip(buildTooltipState(visibleTooltip.idx, visibleTooltip.anchorEl, visibleTooltip.requestIdentity));
    };

    window.addEventListener('resize', updateTooltipPosition);
    window.addEventListener('scroll', updateTooltipPosition, true);
    return () => {
      window.removeEventListener('resize', updateTooltipPosition);
      window.removeEventListener('scroll', updateTooltipPosition, true);
    };
  }, [buildTooltipState, visibleTooltip]);

  const openTooltip = useCallback(
    (idx: number, anchorEl: HTMLDivElement) => {
      setActiveTooltip(buildTooltipState(idx, anchorEl, requestIdentity));
    },
    [buildTooltipState, requestIdentity]
  );

  const handlePointerEnter = useCallback(
    (e: React.PointerEvent<HTMLDivElement>, idx: number) => {
      if (e.pointerType === 'mouse') {
        openTooltip(idx, e.currentTarget);
      }
    },
    [openTooltip]
  );

  const handlePointerLeave = useCallback((e: React.PointerEvent) => {
    if (e.pointerType === 'mouse') {
      setActiveTooltip(null);
    }
  }, []);

  const handlePointerDown = useCallback(
    (e: React.PointerEvent<HTMLDivElement>, idx: number) => {
      if (e.pointerType === 'touch') {
        e.preventDefault();
        const anchorEl = e.currentTarget;
        setActiveTooltip((prev) => (
          prev?.idx === idx && prev.requestIdentity === requestIdentity
            ? null
            : buildTooltipState(idx, anchorEl, requestIdentity)
        ));
      }
    },
    [buildTooltipState, requestIdentity]
  );

  const renderTooltip = (detail: StatusBlockDetail, tooltipState: ActiveTooltipState) => {
    const total = detail.success + detail.failure;
    const posClass =
      tooltipState.horizontal === 'left'
        ? styles.healthTooltipLeft
        : tooltipState.horizontal === 'right'
          ? styles.healthTooltipRight
          : '';
    const vertClass = tooltipState.vertical === 'below' ? styles.healthTooltipBelow : '';
    const timeRange = `${formatDateTime(detail.startTime, dateTimeFormatter)} – ${formatDateTime(detail.endTime, dateTimeFormatter)}`;
    const tooltip = (
      <div
        role="tooltip"
        className={`${styles.healthTooltip} ${posClass} ${vertClass}`}
        style={{
          position: 'fixed',
          left: `${tooltipState.left}px`,
          top: `${tooltipState.top}px`,
          bottom: 'auto',
          right: 'auto',
          transform: tooltipState.transform,
        }}
      >
        <span className={styles.healthTooltipTime}>{timeRange}</span>
        {total > 0 ? (
          <span className={styles.healthTooltipStats}>
            <span className={styles.healthTooltipSuccess}>
              {t('status_bar.success_short')} {detail.success}
            </span>
            <span className={styles.healthTooltipFailure}>
              {t('status_bar.failure_short')} {detail.failure}
            </span>
            <span className={styles.healthTooltipRate}>({(detail.rate * 100).toFixed(1)}%)</span>
          </span>
        ) : (
          <span className={styles.healthTooltipStats}>{t('status_bar.no_requests')}</span>
        )}
      </div>
    );

    return typeof document === 'undefined' ? tooltip : createPortal(tooltip, document.body);
  };

  const rateClass = !hasData
    ? ''
    : healthData.successRate >= 90
      ? styles.healthRateHigh
      : healthData.successRate >= 50
        ? styles.healthRateMedium
        : styles.healthRateLow;

  const windowLabel = healthData.windowStart > 0 && healthData.windowEnd > 0
    ? `${formatDateTime(healthData.windowStart, dateTimeFormatter)} – ${formatDateTime(healthData.windowEnd, dateTimeFormatter)}`
    : t('usage_stats.service_health_window');
  const healthCountsLabel = `${t('status_bar.success_short')} ${healthData.totalSuccess}, ${t('status_bar.failure_short')} ${healthData.totalFailure}`;
  const gridStyle = useMemo(
    () => ({
      '--health-grid-columns': String(ACTIVITY_GRID_COLUMNS),
      '--health-grid-rows': String(ACTIVITY_GRID_ROWS),
      '--health-grid-aspect-columns': String(ACTIVITY_GRID_COLUMNS),
      '--health-grid-aspect-rows': String(ACTIVITY_GRID_ROWS),
      '--health-grid-width': '100%',
    }) as React.CSSProperties,
    []
  );

  return (
    <div className={styles.healthCard}>
      <div className={styles.healthHeader}>
        <ServiceHealthTitle
          title={t('usage_stats.service_health_title')}
          subtitle={t('usage_stats.service_health_subtitle')}
        />
        <div className={styles.healthMeta}>
          <div className={styles.healthTopLine}>
            <span className={styles.healthWindow}>{windowLabel}</span>
            <span className={`${styles.healthRate} ${rateClass}`}>
              {loading ? '--' : hasData ? `${healthData.successRate.toFixed(1)}%` : '--'}
            </span>
          </div>
          <div className={styles.healthMetricPanel} aria-label={healthCountsLabel}>
            <span className={styles.healthCountRow}>
              <span className={`${styles.healthCountDot} ${styles.healthCountDotSuccess}`} aria-hidden="true" />
              <span>{healthData.totalSuccess}</span>
            </span>
            <span className={styles.healthCountRow}>
              <span className={`${styles.healthCountDot} ${styles.healthCountDotFailure}`} aria-hidden="true" />
              <span>{healthData.totalFailure}</span>
            </span>
          </div>
        </div>
      </div>
      <div className={styles.healthGridScroller}>
        <div className={styles.healthGrid} ref={gridRef} style={gridStyle} role="list" aria-label={healthCountsLabel}>
          {healthData.blockDetails.map((detail, idx) => {
            const isIdle = detail.rate === -1;
            const blockStyle = isIdle ? undefined : { backgroundColor: rateToColor(detail.rate) };
            const isActive = visibleTooltip?.idx === idx;
            const timeRange = `${formatDateTime(detail.startTime, dateTimeFormatter)} – ${formatDateTime(detail.endTime, dateTimeFormatter)}`;
            const summary = detail.success + detail.failure > 0
              ? `${t('status_bar.success_short')} ${detail.success}, ${t('status_bar.failure_short')} ${detail.failure}`
              : t('status_bar.no_requests');

            return (
              <div
                key={idx}
                className={`${styles.healthBlockWrapper} ${isActive ? styles.healthBlockActive : ''}`}
                role="listitem"
                tabIndex={0}
                aria-label={t('usage_stats.service_health_block_label', { timeRange, summary })}
                onFocus={(e) => openTooltip(idx, e.currentTarget)}
                onBlur={() => setActiveTooltip(null)}
                onPointerEnter={(e) => handlePointerEnter(e, idx)}
                onPointerLeave={handlePointerLeave}
                onPointerDown={(e) => handlePointerDown(e, idx)}
              >
                <div
                  className={`${styles.healthBlock} ${isIdle ? styles.healthBlockIdle : ''}`}
                  style={blockStyle}
                />
                {isActive && visibleTooltip && renderTooltip(detail, visibleTooltip)}
              </div>
            );
          })}
        </div>
      </div>
      <div className={styles.healthLegend}>
        <span className={styles.healthLegendLabel}>{t('usage_stats.service_health_oldest')}</span>
        <div className={styles.healthLegendColors}>
          <div className={`${styles.healthLegendBlock} ${styles.healthBlockIdle}`} />
          <div className={styles.healthLegendBlock} style={{ backgroundColor: '#ef4444' }} />
          <div className={styles.healthLegendBlock} style={{ backgroundColor: '#facc15' }} />
          <div className={styles.healthLegendBlock} style={{ backgroundColor: '#22c55e' }} />
        </div>
        <span className={styles.healthLegendLabel}>{t('usage_stats.service_health_newest')}</span>
      </div>
    </div>
  );
}
