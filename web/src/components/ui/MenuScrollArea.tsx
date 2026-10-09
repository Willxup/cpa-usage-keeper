import { useLayoutEffect, useRef, type AriaRole, type ReactNode } from 'react';
import styles from './MenuScrollArea.module.scss';

const MAX_PULL = 32;
const RESISTANCE = 90;
const TRACK_INSET = 6;
const clamp = (value: number, min: number, max: number) => Math.min(max, Math.max(min, value));

interface MenuScrollAreaProps {
  children: ReactNode;
  id?: string;
  role?: AriaRole;
  ariaLabel?: string;
}

export function MenuScrollArea({ children, id, role, ariaLabel }: MenuScrollAreaProps) {
  const rootRef = useRef<HTMLDivElement>(null);
  const viewportRef = useRef<HTMLDivElement>(null);
  const contentRef = useRef<HTMLDivElement>(null);
  const trackRef = useRef<HTMLDivElement>(null);
  const thumbRef = useRef<HTMLDivElement>(null);

  useLayoutEffect(() => {
    const root = rootRef.current!;
    const viewport = viewportRef.current!;
    const content = contentRef.current!;
    const track = trackRef.current!;
    const thumb = thumbRef.current!;
    const motion = window.matchMedia('(prefers-reduced-motion: reduce)');
    let range = 0;
    let trackHeight = 0;
    let thumbHeight = 0;
    let rawPull = 0;
    let pull = 0;
    let velocity = 0;
    let frame = 0;
    let measureFrame = 0;
    let suppressClickUntil = 0;
    let inertiaUntil = 0;
    let lastScrollTop = 0;
    let lastScrollAt = 0;
    let nativeVelocity = 0;
    let touch: { id: number; x: number; y: number; startY: number; elastic: boolean } | null = null;
    let drag: { id: number; y: number; top: number } | null = null;

    const paint = () => {
      viewport.style.transform = pull ? `translate3d(0, ${pull.toFixed(3)}px, 0)` : '';
      const height = Math.max(Math.min(12, thumbHeight), thumbHeight - Math.abs(pull) * 1.25);
      const progress = range ? clamp(viewport.scrollTop / range, 0, 1) : 0;
      thumb.style.height = `${height}px`;
      thumb.style.transform = `translateY(${progress * (trackHeight - height)}px)`;
      root.dataset.pulling = String(Math.abs(pull) > 0.1);
    };
    const stop = () => {
      cancelAnimationFrame(frame);
      frame = 0;
    };
    const reset = () => {
      stop();
      inertiaUntil = 0;
      rawPull = pull = velocity = 0;
      paint();
    };
    const release = () => {
      if (frame) return;
      rawPull = 0;
      velocity = 0;
      if (motion.matches || Math.abs(pull) < 0.1) { reset(); return; }
      let previousTime = performance.now();
      // 位移和滑块压缩共用同一弹簧数值，避免两者恢复速度不一致。
      const spring = (now: number) => {
        const dt = Math.min((now - previousTime) / 1000, 0.032);
        previousTime = now;
        velocity += (-280 * pull - 32 * velocity) * dt;
        pull += velocity * dt;
        if (Math.abs(pull) < 0.1 && Math.abs(velocity) < 0.5) { reset(); return; }
        paint();
        frame = requestAnimationFrame(spring);
      };
      frame = requestAnimationFrame(spring);
    };
    const stretch = (distance: number) => {
      if (motion.matches) return;
      if (frame) {
        rawPull = -Math.sign(pull) * RESISTANCE * Math.log(1 - Math.min(Math.abs(pull) / MAX_PULL, 0.999));
      }
      stop();
      rawPull = clamp(rawPull + distance, -500, 500);
      pull = Math.sign(rawPull) * MAX_PULL * (1 - Math.exp(-Math.abs(rawPull) / RESISTANCE));
      paint();
    };
    const wheelRebound = (distance: number) => {
      if (motion.matches) return;
      const nextPull = Math.sign(distance) * MAX_PULL * (1 - Math.exp(-Math.abs(distance) / RESISTANCE));
      // 滚轮每次输入只补充更强的压缩，惯性尾部的小事件不累积拉力或重启弹簧。
      if (Math.sign(nextPull) !== Math.sign(pull) || Math.abs(nextPull) > Math.abs(pull)) {
        pull = nextPull;
        velocity = 0;
        paint();
      }
      release();
    };
    const measure = () => {
      const nextRange = Math.max(0, viewport.scrollHeight - viewport.clientHeight);
      const nextTrackHeight = Math.max(0, viewport.clientHeight - TRACK_INSET * 2);
      if (range !== nextRange || trackHeight !== nextTrackHeight) reset();
      range = nextRange;
      trackHeight = nextTrackHeight;
      thumbHeight = Math.min(trackHeight, Math.max(24, trackHeight * viewport.clientHeight / Math.max(1, viewport.scrollHeight)));
      root.dataset.overflow = String(range > 0);
      track.hidden = range <= 0;
      paint();
    };
    const scroll = () => {
      const now = performance.now();
      const top = clamp(viewport.scrollTop, 0, range);
      const elapsed = now - lastScrollAt;
      if (elapsed > 0) nativeVelocity = nativeVelocity * 0.5 + (top - lastScrollTop) / elapsed * 0.5;
      // 松手后的原生惯性撞到边界时补一次反馈，不接管范围内的滑动速度。
      if (!touch && now < inertiaUntil && !frame && !pull &&
          ((top === 0 && lastScrollTop > 0) || (top === range && lastScrollTop < range))) {
        stretch((top === 0 ? 1 : -1) * Math.min(140, Math.abs(nativeVelocity) * 70));
        inertiaUntil = 0;
        release();
      }
      lastScrollTop = top;
      lastScrollAt = now;
      // 手指反向滑回正常范围时，让原生滚动接续，不保留越界位移。
      if (touch && viewport.scrollTop > 0 && viewport.scrollTop < range && pull && !frame) release();
      paint();
    };
    const wheel = (event: WheelEvent) => {
      inertiaUntil = 0;
      if (!range || event.ctrlKey || event.shiftKey || Math.abs(event.deltaX) > Math.abs(event.deltaY)) return;
      const delta = event.deltaY * (event.deltaMode === 1 ? 16 : event.deltaMode === 2 ? viewport.clientHeight : 1);
      const next = clamp(viewport.scrollTop, 0, range) + delta;
      const excess = next < 0 ? -next : next > range ? range - next : 0;
      if (!excess) { if (pull) release(); return; }
      // 正常范围内不接管滚轮；越界的剩余距离才转成阻尼位移。
      event.preventDefault();
      viewport.scrollTo({ top: clamp(next, 0, range), behavior: 'instant' });
      wheelRebound(excess);
    };
    const touchStart = (event: TouchEvent) => {
      suppressClickUntil = 0;
      if (event.touches.length !== 1 || track.contains(event.target as Node)) { touch = null; release(); return; }
      const point = event.touches[0];
      reset();
      lastScrollTop = viewport.scrollTop;
      lastScrollAt = performance.now();
      nativeVelocity = 0;
      touch = { id: point.identifier, x: point.clientX, y: point.clientY, startY: point.clientY, elastic: false };
    };
    const touchMove = (event: TouchEvent) => {
      if (!touch || event.touches.length !== 1 || !range) return;
      const point = event.touches[0];
      if (point.identifier !== touch.id) return;
      const distance = point.clientY - touch.y;
      const horizontal = point.clientX - touch.x;
      touch.x = point.clientX;
      touch.y = point.clientY;
      if (Math.abs(horizontal) > Math.abs(distance)) return;
      if (rawPull && event.cancelable) {
        event.preventDefault();
        const nextPull = rawPull + distance;
        if (Math.sign(nextPull) === Math.sign(rawPull)) stretch(distance);
        else {
          reset();
          viewport.scrollTo({ top: clamp(viewport.scrollTop - nextPull, 0, range), behavior: 'instant' });
        }
        return;
      }
      const next = clamp(viewport.scrollTop, 0, range) - distance;
      const excess = next < 0 ? -next : next > range ? range - next : 0;
      if (!excess) { if (pull && !frame) release(); return; }
      if (event.cancelable) event.preventDefault();
      viewport.scrollTo({ top: clamp(next, 0, range), behavior: 'instant' });
      touch.elastic = true;
      stretch(excess);
    };
    const touchEnd = () => {
      const nativeGesture = touch && !touch.elastic;
      if (touch?.elastic && Math.abs(touch.y - touch.startY) > 6) suppressClickUntil = performance.now() + 350;
      touch = null;
      release();
      if (nativeGesture) inertiaUntil = performance.now() + 1400;
    };
    const click = (event: MouseEvent) => {
      if (event.detail > 0 && performance.now() < suppressClickUntil) {
        event.preventDefault();
        event.stopPropagation();
      }
      reset();
    };
    const pointerDown = (event: PointerEvent) => {
      if (event.button !== 0 || !range) return;
      event.preventDefault();
      event.stopPropagation();
      reset();
      if (!thumb.contains(event.target as Node)) {
        const position = event.clientY - track.getBoundingClientRect().top - thumbHeight / 2;
        viewport.scrollTo({ top: clamp(position / Math.max(1, trackHeight - thumbHeight), 0, 1) * range, behavior: 'instant' });
      }
      drag = { id: event.pointerId, y: event.clientY, top: viewport.scrollTop };
      track.setPointerCapture(event.pointerId);
      root.dataset.dragging = 'true';
    };
    const pointerMove = (event: PointerEvent) => {
      if (!drag || event.pointerId !== drag.id) return;
      viewport.scrollTo({ top: clamp(drag.top + (event.clientY - drag.y) * range / Math.max(1, trackHeight - thumbHeight), 0, range), behavior: 'instant' });
    };
    const pointerEnd = () => { drag = null; root.dataset.dragging = 'false'; };

    // 滑块占位可能改变文字换行，将观察后的样式更新放到下一帧，避免观察循环。
    const observer = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(() => {
      cancelAnimationFrame(measureFrame);
      measureFrame = requestAnimationFrame(() => { measureFrame = 0; measure(); });
    });
    observer?.observe(viewport);
    observer?.observe(content);
    measure();
    window.addEventListener('resize', measure);
    motion.addEventListener('change', reset);
    viewport.addEventListener('scroll', scroll, { passive: true });
    root.addEventListener('wheel', wheel, { passive: false });
    root.addEventListener('touchstart', touchStart, { passive: true });
    root.addEventListener('touchmove', touchMove, { passive: false });
    root.addEventListener('touchend', touchEnd);
    root.addEventListener('touchcancel', touchEnd);
    root.addEventListener('click', click, true);
    document.addEventListener('keydown', reset, true);
    track.addEventListener('pointerdown', pointerDown);
    track.addEventListener('pointermove', pointerMove);
    track.addEventListener('pointerup', pointerEnd);
    track.addEventListener('lostpointercapture', pointerEnd);
    return () => {
      stop();
      cancelAnimationFrame(measureFrame);
      observer?.disconnect();
      window.removeEventListener('resize', measure);
      motion.removeEventListener('change', reset);
      viewport.removeEventListener('scroll', scroll);
      root.removeEventListener('wheel', wheel);
      root.removeEventListener('touchstart', touchStart);
      root.removeEventListener('touchmove', touchMove);
      root.removeEventListener('touchend', touchEnd);
      root.removeEventListener('touchcancel', touchEnd);
      root.removeEventListener('click', click, true);
      document.removeEventListener('keydown', reset, true);
      track.removeEventListener('pointerdown', pointerDown);
      track.removeEventListener('pointermove', pointerMove);
      track.removeEventListener('pointerup', pointerEnd);
      track.removeEventListener('lostpointercapture', pointerEnd);
    };
  }, []);

  return <div ref={rootRef} className={styles.root} data-menu-scroll-area>
    <div ref={viewportRef} className={styles.viewport} id={id} role={role} aria-label={ariaLabel} data-menu-scroll-viewport>
      <div ref={contentRef} className={styles.content}>{children}</div>
    </div>
    <div ref={trackRef} className={styles.track} aria-hidden="true" data-menu-scrollbar hidden>
      <div ref={thumbRef} className={styles.thumb} data-menu-scroll-thumb />
    </div>
  </div>;
}
