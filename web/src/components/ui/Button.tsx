import React, { type ButtonHTMLAttributes, type PropsWithChildren } from 'react';

type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger';
type ButtonSize = 'md' | 'sm';
type ButtonAppearance = 'default' | 'action';

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant;
  size?: ButtonSize;
  appearance?: ButtonAppearance;
  fullWidth?: boolean;
  loading?: boolean;
}

export function Button({
  children,
  variant = 'primary',
  size = 'md',
  appearance = 'default',
  fullWidth = false,
  loading = false,
  className = '',
  disabled,
  ...rest
}: PropsWithChildren<ButtonProps>) {
  const hasChildren = children !== null && children !== undefined && children !== false;
  const classes = [
    'btn',
    `btn-${variant}`,
    appearance === 'action' ? 'btn-action' : '',
    size === 'sm' ? 'btn-sm' : '',
    fullWidth ? 'btn-full' : '',
    className
  ]
    .filter(Boolean)
    .join(' ');

  return (
    <button className={classes} disabled={disabled || loading} {...rest}>
      {loading && <span className="loading-spinner" aria-hidden="true" />}
      {hasChildren && <span>{children}</span>}
    </button>
  );
}
