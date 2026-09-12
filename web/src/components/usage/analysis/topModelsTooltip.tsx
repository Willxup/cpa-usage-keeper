import { useCallback, useEffect, useRef, useState, type PointerEvent, type RefObject } from 'react';
import { createPortal } from 'react-dom';
import { useTranslation } from 'react-i18next';
import type { Chart, TooltipModel } from 'chart.js';
import { IconX } from '@/components/ui/icons';
import { useAnchorPosition } from '@/hooks/useAnchorPosition';
import styles from './AnalysisPanel.module.scss';

type TooltipContent = {
  source: object;
  signature: string;
  x: number;
  y: number;
  touch: boolean;
  title: string;
  rows: { text: string; color: string }[];
  footer: string;
};

function TopModelsTooltip({ content, canvasRef, panelRef, isMobile, dismiss, cancelClose, scheduleClose }: {
  content: TooltipContent;
  canvasRef: RefObject<HTMLCanvasElement | null>;
  panelRef: RefObject<HTMLDivElement | null>;
  isMobile: boolean;
  dismiss: () => void;
  cancelClose: () => void;
  scheduleClose: () => void;
}) {
  const { t } = useTranslation();
  const updatePosition = useCallback((rect: DOMRect) => {
    const panel = panelRef.current;
    if (!panel) return;
    const viewport = window.visualViewport;
    const width = viewport?.width ?? window.innerWidth;
    const height = viewport?.height ?? window.innerHeight;
    const leftEdge = (viewport?.offsetLeft ?? 0) + 8;
    const topEdge = (viewport?.offsetTop ?? 0) + 8;
    const mobile = isMobile || content.touch;
    panel.style.width = `${Math.min(mobile ? width : 360, width - 16)}px`;
    panel.style.maxHeight = `${Math.min(380, mobile ? height * 0.6 : height - 16)}px`;
    const bounds = panel.getBoundingClientRect();
    const rightEdge = leftEdge + width - 16;
    const bottomEdge = topEdge + height - 16;
    const anchorX = rect.left + content.x;
    const anchorY = rect.top + content.y;
    const preferredLeft = anchorX + 10 + bounds.width <= rightEdge ? anchorX + 10 : anchorX - bounds.width - 10;
    const left = mobile ? leftEdge : Math.max(leftEdge, Math.min(preferredLeft, rightEdge - bounds.width));
    const top = mobile ? bottomEdge - bounds.height : Math.max(topEdge, Math.min(anchorY - bounds.height / 2, bottomEdge - bounds.height));
    // 使用页面坐标并跟踪可视视口，兼容手机地址栏收缩、缩放与页面滚动。
    panel.style.left = `${left + window.scrollX}px`;
    panel.style.top = `${top + window.scrollY}px`;
  }, [content, isMobile, panelRef]);
  useAnchorPosition(true, canvasRef, updatePosition);

  return (
    <div ref={panelRef} role="tooltip" data-top-models-tooltip className={styles.topModelsTooltip}
      onPointerEnter={cancelClose} onPointerLeave={scheduleClose}>
      <div className={styles.topModelsTooltipHeader}>
        <strong>{content.title}</strong>
        <button type="button" aria-label={t('common.close')} onClick={dismiss}><IconX size={16} /></button>
      </div>
      <ol key={content.title} className={styles.topModelsTooltipRows}>
        {content.rows.map((row, index) => (
          <li key={index}>
            <span className={styles.topModelsTooltipDot} style={{ backgroundColor: row.color }} aria-hidden="true" />
            <span>{row.text}</span>
          </li>
        ))}
      </ol>
      <strong className={styles.topModelsTooltipFooter}>{content.footer}</strong>
    </div>
  );
}

export function useTopModelsTooltip(source: object, enabled: boolean, isMobile: boolean) {
  const chartRef = useRef<Chart | null>(null);
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const panelRef = useRef<HTMLDivElement | null>(null);
  const touchInput = useRef(false);
  const hoveringTooltip = useRef(false);
  const closeTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const [content, setContent] = useState<TooltipContent | null>(null);
  const visible = enabled && content?.source === source ? content : null;

  const cancelClose = useCallback(() => {
    if (closeTimer.current !== null) clearTimeout(closeTimer.current);
    closeTimer.current = null;
  }, []);
  const dismiss = useCallback(() => {
    hoveringTooltip.current = false;
    const chart = chartRef.current;
    if (chart?.canvas) {
      // 同步清除 Chart.js 的活动项，关闭后再次点同一根柱子也能重新打开。
      chart.tooltip?.setActiveElements([], { x: 0, y: 0 });
      chart.setActiveElements([]);
      chart.draw();
    }
    cancelClose();
    setContent(null);
  }, [cancelClose]);
  const scheduleClose = useCallback(() => {
    // Chart.js 的 mouseout 可能晚于浮层 pointerenter 到达，进入浮层后仍须保持打开。
    cancelClose();
    if (touchInput.current || hoveringTooltip.current) return;
    // 留出从画布移入浮层的时间，让鼠标能滚动查看完整列表。
    closeTimer.current = setTimeout(dismiss, 160);
  }, [cancelClose, dismiss]);
  const enterTooltip = useCallback(() => {
    hoveringTooltip.current = true;
    cancelClose();
  }, [cancelClose]);
  const leaveTooltip = useCallback(() => {
    hoveringTooltip.current = false;
    scheduleClose();
  }, [scheduleClose]);

  const external = useCallback(({ chart, tooltip }: { chart: Chart; tooltip: TooltipModel<'bar'> }) => {
    if (tooltip.opacity === 0) {
      scheduleClose();
      return;
    }
    cancelClose();
    if (!panelRef.current) hoveringTooltip.current = false;
    chartRef.current = chart;
    canvasRef.current = chart.canvas;
    const title = tooltip.title.join('\n');
    const rows = tooltip.body.map((body, index) => ({
      text: [...body.before, ...body.lines, ...body.after].join('\n'),
      color: String(tooltip.labelColors[index].borderColor),
    }));
    const footer = tooltip.footer.join('\n');
    const signature = JSON.stringify([title, rows, footer]);
    const { caretX: x, caretY: y } = tooltip;
    const touch = touchInput.current;
    setContent((previous) => previous?.source === source && previous.signature === signature
      && previous.x === x && previous.y === y && previous.touch === touch
      ? previous : { source, signature, title, rows, footer, x, y, touch });
  }, [cancelClose, scheduleClose, source]);

  useEffect(() => {
    if (!visible) return;
    const outside = (event: globalThis.PointerEvent) => {
      if (event.target instanceof Node && event.target !== canvasRef.current && !panelRef.current?.contains(event.target)) dismiss();
    };
    const escape = (event: KeyboardEvent) => { if (event.key === 'Escape') dismiss(); };
    document.addEventListener('pointerdown', outside);
    document.addEventListener('keydown', escape);
    return () => {
      document.removeEventListener('pointerdown', outside);
      document.removeEventListener('keydown', escape);
    };
  }, [dismiss, visible]);
  useEffect(() => cancelClose, [cancelClose]);

  return {
    external,
    onPointerDown: (event: PointerEvent) => { touchInput.current = event.pointerType === 'touch'; },
    tooltip: visible ? createPortal(
      <TopModelsTooltip content={visible} canvasRef={canvasRef} panelRef={panelRef} isMobile={isMobile}
        dismiss={dismiss} cancelClose={enterTooltip} scheduleClose={leaveTooltip} />,
      document.body,
    ) : null,
  };
}
