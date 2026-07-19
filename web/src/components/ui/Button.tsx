import type { ButtonHTMLAttributes, ReactNode } from 'react';

type NativeButtonProps = Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'size'>;

export interface ButtonProps extends NativeButtonProps {
  variant?: 'primary' | 'secondary';
  size?: 'sm' | 'md';
  children: ReactNode;
}

export function Button({ variant = 'secondary', size = 'md', className, children, ...props }: ButtonProps) {
  return (
    <button
      className={`btn btn-${size} btn-${variant} ${className ?? ''}`.trim()}
      {...props}
    >
      {children}
    </button>
  );
}
