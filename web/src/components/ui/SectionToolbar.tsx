import type { ReactNode } from 'react';
import styles from './SectionToolbar.module.scss';

export interface SectionToolbarProps {
  title: ReactNode;
  headingId?: string;
  meta?: ReactNode;
  actions?: ReactNode;
  className?: string;
  headingClassName?: string;
  titleClassName?: string;
  actionsClassName?: string;
  metaClassName?: string;
}

/**
 * Keeper 通用大盘模块标题栏：1:1 统一近期活动、用量分布与实时指标等区块的标题、时间范围与右侧控制区布局。
 */
export function SectionToolbar({
  title,
  headingId,
  meta,
  actions,
  className,
  headingClassName,
  titleClassName,
  actionsClassName,
  metaClassName,
}: SectionToolbarProps) {
  return (
    <div className={[styles.toolbar, className ?? ''].filter(Boolean).join(' ')}>
      <div className={[styles.heading, headingClassName ?? ''].filter(Boolean).join(' ')}>
        <h2 id={headingId} className={[styles.title, titleClassName ?? ''].filter(Boolean).join(' ')}>
          {title}
        </h2>
      </div>
      {(meta || actions) && (
        <div className={[styles.actions, actionsClassName ?? ''].filter(Boolean).join(' ')}>
          {meta && <span className={[styles.meta, metaClassName ?? ''].filter(Boolean).join(' ')}>{meta}</span>}
          {actions}
        </div>
      )}
    </div>
  );
}
