import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import type { UsageActivityBlock } from '@/lib/types';
import styles from '@/pages/UsagePage.module.scss';

const TOOLTIP_OFFSET = 8;
const TOOLTIP_SAFE_WIDTH = 220;
const TOOLTIP_SAFE_HEIGHT = 120;
const ACTIVITY_GRID_ROWS = 7;
const ACTIVITY_GRID_COLUMNS = 52;

type TooltipHorizontalPosition = 'center' | 'left' | 'right';
type TooltipVerticalPosition = 'above' | 'below';

interface ActiveTooltipState {
  index: number;
  requestIdentity: string;
  anchorEl: HTMLDivElement;
  horizontal: TooltipHorizontalPosition;
  vertical: TooltipVerticalPosition;
  left: number;
  top: number;
  transform: string;
}

export function parseActivityTime(value?: string): number {
  if (!value) return 0;
  const nanosecondBoundary = value.match(/^(.*T\d{2}:\d{2}:\d{2})\.9{4,}(Z|[+-]\d{2}:\d{2})$/);
  if (nanosecondBoundary) {
    const rounded = Date.parse(`${nanosecondBoundary[1]}${nanosecondBoundary[2]}`) + 1000;
    return Number.isFinite(rounded) ? rounded : 0;
  }
  const parsed = Date.parse(value);
  return Number.isFinite(parsed) ? parsed : 0;
}

export function createActivityDateTimeFormatter(timeZone?: string): Intl.DateTimeFormat {
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

export function formatActivityDateTime(timestamp: number, formatter: Intl.DateTimeFormat): string {
  const parts = formatter.formatToParts(new Date(timestamp));
  const values = Object.fromEntries(parts.map((part) => [part.type, part.value]));
  return `${values.month}/${values.day} ${values.hour}:${values.minute}`;
}

export interface ActivityHeatmapGridProps {
  blocks: UsageActivityBlock[];
  timeZone?: string;
  requestIdentity: string;
  ariaLabel: string;
  isIdle: (block: UsageActivityBlock, index: number) => boolean;
  getColor: (block: UsageActivityBlock, index: number) => string | undefined;
  getSummary: (block: UsageActivityBlock, index: number) => string;
  renderTooltipStats: (block: UsageActivityBlock, index: number) => ReactNode;
}

export function ActivityHeatmapGrid({
  blocks,
  timeZone,
  requestIdentity,
  ariaLabel,
  isIdle,
  getColor,
  getSummary,
  renderTooltipStats,
}: ActivityHeatmapGridProps) {
  const [activeTooltip, setActiveTooltip] = useState<ActiveTooltipState | null>(null);
  const gridRef = useRef<HTMLDivElement>(null);
  const dateTimeFormatter = useMemo(() => createActivityDateTimeFormatter(timeZone), [timeZone]);
  // render 阶段只对 identity 不一致的旧 tooltip 做一次幂等清理，避免 A→B→A 恢复陈旧状态。
  if (activeTooltip && activeTooltip.requestIdentity !== requestIdentity) {
    setActiveTooltip(null);
  }
  const visibleTooltip = activeTooltip?.requestIdentity === requestIdentity ? activeTooltip : null;

  useEffect(() => {
    if (!visibleTooltip) return;
    const handler = (event: PointerEvent) => {
      if (gridRef.current && !gridRef.current.contains(event.target as Node)) {
        setActiveTooltip(null);
      }
    };
    document.addEventListener('pointerdown', handler);
    return () => document.removeEventListener('pointerdown', handler);
  }, [visibleTooltip]);

  const buildTooltipState = useCallback((index: number, anchorEl: HTMLDivElement | null, identity: string) => {
    if (!anchorEl || !anchorEl.isConnected) return null;
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
      index,
      requestIdentity: identity,
      anchorEl,
      horizontal,
      vertical,
      left: Math.round(left),
      top: Math.round(top),
      transform: `translate(${translateX}, ${translateY})`,
    } satisfies ActiveTooltipState;
  }, []);

  useEffect(() => {
    if (!visibleTooltip) return;
    const updateTooltipPosition = () => {
      if (!document.body.contains(visibleTooltip.anchorEl)) {
        setActiveTooltip(null);
        return;
      }
      setActiveTooltip(buildTooltipState(visibleTooltip.index, visibleTooltip.anchorEl, visibleTooltip.requestIdentity));
    };
    window.addEventListener('resize', updateTooltipPosition);
    window.addEventListener('scroll', updateTooltipPosition, true);
    return () => {
      window.removeEventListener('resize', updateTooltipPosition);
      window.removeEventListener('scroll', updateTooltipPosition, true);
    };
  }, [buildTooltipState, visibleTooltip]);

  const openTooltip = useCallback((index: number, anchorEl: HTMLDivElement) => {
    setActiveTooltip(buildTooltipState(index, anchorEl, requestIdentity));
  }, [buildTooltipState, requestIdentity]);

  const renderTooltip = (block: UsageActivityBlock, tooltipState: ActiveTooltipState) => {
    const startTime = parseActivityTime(block.start_time);
    const endTime = parseActivityTime(block.end_time);
    const timeRange = `${formatActivityDateTime(startTime, dateTimeFormatter)} – ${formatActivityDateTime(endTime, dateTimeFormatter)}`;
    const horizontalClass = tooltipState.horizontal === 'left'
      ? styles.activityTooltipLeft
      : tooltipState.horizontal === 'right'
        ? styles.activityTooltipRight
        : '';
    const verticalClass = tooltipState.vertical === 'below' ? styles.activityTooltipBelow : '';
    const tooltip = (
      <div
        role="tooltip"
        className={`${styles.activityTooltip} ${horizontalClass} ${verticalClass}`.trim()}
        style={{
          position: 'fixed',
          left: `${tooltipState.left}px`,
          top: `${tooltipState.top}px`,
          bottom: 'auto',
          right: 'auto',
          transform: tooltipState.transform,
        }}
      >
        <span className={styles.activityTooltipTime}>{timeRange}</span>
        {renderTooltipStats(block, tooltipState.index)}
      </div>
    );
    return typeof document === 'undefined' ? tooltip : createPortal(tooltip, document.body);
  };

  return (
    <div className={styles.activityHeatmapScroller}>
      <div
        ref={gridRef}
        className={styles.activityHeatmapGrid}
        role="grid"
        aria-label={ariaLabel}
        aria-rowcount={ACTIVITY_GRID_ROWS}
        aria-colcount={ACTIVITY_GRID_COLUMNS}
      >
        {blocks.map((block, index) => {
          const idle = isIdle(block, index);
          const active = visibleTooltip?.index === index;
          const startTime = parseActivityTime(block.start_time);
          const endTime = parseActivityTime(block.end_time);
          const timeRange = `${formatActivityDateTime(startTime, dateTimeFormatter)} – ${formatActivityDateTime(endTime, dateTimeFormatter)}`;
          return (
            <div
              // 网格固定为 364 个位置槽，窗口变化只更新槽内数据，不替换全部 DOM 节点。
              key={index}
              className={`${styles.activityHeatmapCell} ${active ? styles.activityHeatmapCellActive : ''}`.trim()}
              role="gridcell"
              tabIndex={0}
              aria-rowindex={(index % ACTIVITY_GRID_ROWS) + 1}
              aria-colindex={Math.floor(index / ACTIVITY_GRID_ROWS) + 1}
              aria-label={`${timeRange}: ${getSummary(block, index)}`}
              data-activity-index={index}
              data-activity-start={block.start_time}
              data-activity-end={block.end_time}
              onFocus={(event) => openTooltip(index, event.currentTarget)}
              onBlur={() => setActiveTooltip(null)}
              onPointerEnter={(event) => {
                if (event.pointerType === 'mouse') openTooltip(index, event.currentTarget);
              }}
              onPointerLeave={(event) => {
                if (event.pointerType === 'mouse') setActiveTooltip(null);
              }}
              onPointerDown={(event) => {
                if (event.pointerType !== 'touch') return;
                event.preventDefault();
                const anchorEl = event.currentTarget;
                setActiveTooltip((previous) => (
                  previous?.index === index && previous.requestIdentity === requestIdentity
                    ? null
                    : buildTooltipState(index, anchorEl, requestIdentity)
                ));
              }}
            >
              <div
                className={`${styles.activityHeatmapBlock} ${idle ? styles.activityHeatmapBlockIdle : ''}`.trim()}
                style={idle ? undefined : { backgroundColor: getColor(block, index) }}
              />
              {active && visibleTooltip && renderTooltip(block, visibleTooltip)}
            </div>
          );
        })}
      </div>
    </div>
  );
}
