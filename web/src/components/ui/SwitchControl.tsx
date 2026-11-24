import {
  forwardRef,
  type ChangeEvent,
  type InputHTMLAttributes,
  type ReactNode,
} from 'react';
import styles from './SwitchControl.module.scss';

type NativeSwitchInputProps = Omit<
  InputHTMLAttributes<HTMLInputElement>,
  'type' | 'checked' | 'disabled' | 'onChange'
> & Record<string, unknown>;

export interface SwitchControlProps {
  checked: boolean;
  onChange: (checked: boolean, event: ChangeEvent<HTMLInputElement>) => void;
  label?: ReactNode;
  variant?: 'default' | 'pill';
  disabled?: boolean;
  ariaLabel?: string;
  className?: string;
  labelClassName?: string;
  trackClassName?: string;
  thumbClassName?: string;
  inputProps?: NativeSwitchInputProps;
}

/**
 * Keeper 通用状态开关：1:1 复用原有 42×24 蓝青渐变轨道与圆钮视觉，统一列显示与凭证开关实现。
 */
export const SwitchControl = forwardRef<HTMLInputElement, SwitchControlProps>(function SwitchControl(
  {
    checked,
    onChange,
    label,
    variant = 'default',
    disabled = false,
    ariaLabel,
    className,
    labelClassName,
    trackClassName,
    thumbClassName,
    inputProps,
  },
  ref,
) {
  const rootClassName = [
    styles.root,
    variant === 'pill' ? styles.pill : '',
    label === undefined ? styles.iconOnly : '',
    disabled ? styles.disabled : '',
    className ?? '',
  ].filter(Boolean).join(' ');

  return (
    <label className={rootClassName}>
      {label !== undefined && (
        <span className={[styles.label, labelClassName ?? ''].filter(Boolean).join(' ')}>
          {label}
        </span>
      )}
      <input
        {...inputProps}
        ref={ref}
        type="checkbox"
        className={styles.input}
        checked={checked}
        disabled={disabled}
        aria-label={ariaLabel ?? (typeof inputProps?.['aria-label'] === 'string' ? inputProps['aria-label'] : undefined)}
        onChange={(event) => onChange(event.target.checked, event)}
      />
      <span className={[styles.track, trackClassName ?? ''].filter(Boolean).join(' ')} aria-hidden="true">
        <span className={[styles.thumb, thumbClassName ?? ''].filter(Boolean).join(' ')} />
      </span>
    </label>
  );
});
