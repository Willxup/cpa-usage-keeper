import type { ComponentProps } from 'react';
import { Button } from './Button';

type MainActionButtonProps = Omit<ComponentProps<typeof Button>, 'appearance' | 'size' | 'variant'> & {
  shellClassName?: string;
};

export function MainActionButton({
  children,
  className = '',
  shellClassName = '',
  loading = false,
  ...rest
}: MainActionButtonProps) {
  const shellClasses = ['main-action-button-shell', shellClassName].filter(Boolean).join(' ');
  const buttonClasses = ['main-action-button', className].filter(Boolean).join(' ');

  return (
    <span className={shellClasses}>
      <Button
        {...rest}
        variant="primary"
        appearance="action"
        className={buttonClasses}
        loading={loading}
        aria-busy={loading || rest['aria-busy'] || undefined}
      >
        {children}
      </Button>
    </span>
  );
}
