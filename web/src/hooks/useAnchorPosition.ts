import { useLayoutEffect, type RefObject } from 'react';

// 弹层打开期间跟踪真实位置；工具条换行和动画只改变位置时，ResizeObserver 不会通知。
export function useAnchorPosition(
  open: boolean,
  anchorRef: RefObject<HTMLElement | null>,
  updatePosition: (rect: DOMRect) => void,
) {
  useLayoutEffect(() => {
    if (!open) return;
    let frame = 0;
    const viewport = window.visualViewport;
    let previous: {
      rect: DOMRect;
      width: number;
      height: number;
      visibleWidth: number;
      visibleHeight: number;
      left: number;
      top: number;
      scrollX: number;
      scrollY: number;
    } | undefined;
    const update = () => {
      const anchor = anchorRef.current;
      if (!anchor) return;
      const rect = anchor.getBoundingClientRect();
      const width = window.innerWidth;
      const height = window.innerHeight;
      const visibleWidth = viewport?.width ?? width;
      const visibleHeight = viewport?.height ?? height;
      const left = viewport?.offsetLeft ?? 0;
      const top = viewport?.offsetTop ?? 0;
      const { scrollX, scrollY } = window;
      if (!previous || rect.x !== previous.rect.x || rect.y !== previous.rect.y
        || rect.width !== previous.rect.width || rect.height !== previous.rect.height
        || width !== previous.width || height !== previous.height
        || visibleWidth !== previous.visibleWidth || visibleHeight !== previous.visibleHeight
        || left !== previous.left || top !== previous.top
        || scrollX !== previous.scrollX || scrollY !== previous.scrollY) {
        previous = { rect, width, height, visibleWidth, visibleHeight, left, top, scrollX, scrollY };
        updatePosition(rect);
      }
    };
    const track = () => { update(); frame = requestAnimationFrame(track); };
    track();
    // 父容器换行在尺寸通知阶段即可同步；逐帧检查补充尺寸不变的位移和动画。
    const observer = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(update);
    for (let element = anchorRef.current; element; element = element.parentElement) observer?.observe(element);
    window.addEventListener('resize', update);
    window.addEventListener('scroll', update, true);
    // iOS 键盘与页面缩放只改变可视视口时，也需要重算弹层。
    viewport?.addEventListener('resize', update);
    viewport?.addEventListener('scroll', update);
    return () => {
      cancelAnimationFrame(frame);
      observer?.disconnect();
      window.removeEventListener('resize', update);
      window.removeEventListener('scroll', update, true);
      viewport?.removeEventListener('resize', update);
      viewport?.removeEventListener('scroll', update);
    };
  }, [open, anchorRef, updatePosition]);
}
