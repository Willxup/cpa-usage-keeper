import type { ReactNode } from 'react';
import styles from './PillSwitcher.module.scss';

export interface PillSwitcherOption<T extends string> {
  value: T;
  label: ReactNode;
  disabled?: boolean;
  title?: string;
  dataAttributes?: Readonly<Record<string, string | number | boolean | undefined>>;
}

export interface PillSwitcherProps<T extends string> {
  options: readonly PillSwitcherOption<T>[];
  value: T | null | undefined;
  onChange: (value: T) => void;
  ariaLabel?: string;
  variant?: 'toolbar' | 'compact';
  className?: string;
  optionClassName?: string;
  activeOptionClassName?: string;
}

/**
 * Keeper 通用胶囊分段切换器：1:1 保持既有窗口切换与卡片内维度切换的尺寸、圆角与阴影。
 */
export function PillSwitcher<T extends string>({
  options,
  value,
  onChange,
  ariaLabel,
  variant = 'toolbar',
  className,
  optionClassName,
  activeOptionClassName,
}: PillSwitcherProps<T>) {
  const rootClassName = [
    styles[variant],
    className ?? '',
  ].filter(Boolean).join(' ');

  return (
    <div className={rootClassName} role="group" aria-label={ariaLabel}>
      {options.map((option) => {
        const isActive = value === option.value;
        const buttonClassName = [
          styles.option,
          optionClassName ?? '',
          isActive ? styles.active : '',
          isActive && activeOptionClassName ? activeOptionClassName : '',
        ].filter(Boolean).join(' ');

        const extraAttributes: Record<string, string | number | boolean | undefined> = {};
        if (option.dataAttributes) {
          for (const [key, attrValue] of Object.entries(option.dataAttributes)) {
            if (attrValue !== undefined) extraAttributes[key] = attrValue;
          }
        }

        return (
          <button
            key={option.value}
            type="button"
            disabled={option.disabled}
            title={option.title}
            className={buttonClassName}
            onClick={() => onChange(option.value)}
            {...extraAttributes}
            aria-pressed={isActive}
          >
            {option.label}
          </button>
        );
      })}
    </div>
  );
}
