import { useId, useLayoutEffect, useRef } from 'react';
import styles from './GlassSurface.module.scss';

export function GlassSurface() {
  const id = `toolbar-lens-${useId().replace(/:/g, '')}`;
  const surfaceRef = useRef<HTMLDivElement>(null);
  const imageRef = useRef<SVGFEImageElement>(null);
  const filterRef = useRef<SVGFilterElement>(null);

  useLayoutEffect(() => {
    const surface = surfaceRef.current;
    if (!surface) return;
    let redrawTimer: ReturnType<typeof setTimeout>;
    let previousSize = '';
    const update = () => {
      const width = Math.round(surface.clientWidth);
      const height = Math.round(surface.clientHeight);
      if (!width || !height) return;
      const radius = Math.min(parseFloat(getComputedStyle(surface).borderTopLeftRadius) || 26, width / 2, height / 2);
      const signature = `${width}:${height}:${radius}`;
      if (signature === previousSize) return;
      previousSize = signature;
      const edge = Math.min(13, radius * .5);
      const ratio = Math.min(2, window.devicePixelRatio || 1);
      const canvas = document.createElement('canvas');
      canvas.width = Math.round(width * ratio);
      canvas.height = Math.round(height * ratio);
      const context = canvas.getContext('2d');
      if (!context) return;
      const map = context.createImageData(canvas.width, canvas.height);
      // 沿用 v2 Demo 的向内正弦折射：轮廓与内沿归零，中间最大位移 10px。
      // 画布仅生成位移场，页面背景仍由浏览器实时合成，不复制业务 DOM。
      for (let y = 0; y < canvas.height; y++) for (let x = 0; x < canvas.width; x++) {
        const cx = (x + .5) / ratio;
        const cy = (y + .5) / ratio;
        const dx = cx - Math.max(radius, Math.min(width - radius, cx));
        const dy = cy - Math.max(radius, Math.min(height - radius, cy));
        const distance = Math.hypot(dx, dy);
        const depth = radius - distance;
        const strength = depth > 0 && depth < edge ? Math.sin(Math.PI * depth / edge) ** .72 : 0;
        const index = (y * canvas.width + x) * 4;
        map.data[index] = 127.5 - (distance ? dx / distance : 0) * strength * 127.5;
        map.data[index + 1] = 127.5 - (distance ? dy / distance : 0) * strength * 127.5;
        map.data[index + 2] = 128; map.data[index + 3] = 255;
      }
      context.putImageData(map, 0, 0);
      imageRef.current?.setAttribute('href', canvas.toDataURL());
      for (const node of [filterRef.current, imageRef.current]) { node?.setAttribute('width', String(width)); node?.setAttribute('height', String(height)); }
    };
    update();
    const observer = new ResizeObserver(() => {
      // 连续缩放时复用位移图，尺寸稳定后再编码，避免每帧 Canvas 运算阻塞过渡。
      for (const node of [filterRef.current, imageRef.current]) {
        node?.setAttribute('width', String(surface.clientWidth));
        node?.setAttribute('height', String(surface.clientHeight));
      }
      clearTimeout(redrawTimer);
      redrawTimer = setTimeout(update, 80);
    });
    observer.observe(surface);
    return () => { observer.disconnect(); clearTimeout(redrawTimer); };
  }, []);

  return <div ref={surfaceRef} className={styles.surface} data-glass-surface aria-hidden="true">
    <div className={styles.base} />
    <div className={styles.lens} style={{ backdropFilter: `url("#${id}")`, WebkitBackdropFilter: `url("#${id}")` }} />
    <div className={styles.rim} />
    <svg className={styles.filter} focusable="false"><defs><filter ref={filterRef} id={id} x="0" y="0" filterUnits="userSpaceOnUse" colorInterpolationFilters="sRGB"><feImage ref={imageRef} x="0" y="0" preserveAspectRatio="none" result="displacement" /><feDisplacementMap in="SourceGraphic" in2="displacement" scale="20" xChannelSelector="R" yChannelSelector="G" /></filter></defs></svg>
  </div>;
}
