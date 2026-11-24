import type { HTMLAttributes, PropsWithChildren } from 'react';
import styles from './ChartSurface.module.scss';

export interface ChartSurfaceProps extends HTMLAttributes<HTMLDivElement> {
  className?: string;
}

/**
 * Keeper 通用图表内框容器：1:1 统一 Analysis 与 Realtime 各卡片内的次级画布底板。
 */
export function ChartSurface({ className, children, ...props }: PropsWithChildren<ChartSurfaceProps>) {
  return (
    <div className={[styles.surface, className ?? ''].filter(Boolean).join(' ')} {...props}>
      {children}
    </div>
  );
}
