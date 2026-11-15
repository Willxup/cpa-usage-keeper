import type { HTMLAttributes, ReactNode } from 'react';
import styles from './MetricChip.module.scss';

export interface MetricChipProps extends Omit<HTMLAttributes<HTMLSpanElement>, 'children'> {
  label: ReactNode;
  value: ReactNode;
  variant?: 'meta' | 'stat';
  tone?: 'default' | 'up' | 'down' | 'flat';
  labelClassName?: string;
  valueClassName?: string;
}

/**
 * Keeper 通用指标胶囊：1:1 统一实时面板统计胶囊与流带/占比列表键值胶囊，固定等宽数字排版。
 */
export function MetricChip({
  label,
  value,
  variant = 'meta',
  tone = 'default',
  className,
  labelClassName,
  valueClassName,
  ...props
}: MetricChipProps) {
  const rootClassName = [
    styles[variant],
    tone !== 'default' ? styles[tone] : '',
    className ?? '',
  ].filter(Boolean).join(' ');

  return (
    <span className={rootClassName} {...props}>
      <span className={[styles.label, labelClassName ?? ''].filter(Boolean).join(' ')}>{label}</span>
      <span className={[styles.value, valueClassName ?? ''].filter(Boolean).join(' ')}>{value}</span>
    </span>
  );
}
