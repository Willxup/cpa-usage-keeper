import React, { type PropsWithChildren, type ReactNode, type HTMLAttributes } from 'react';

interface CardProps extends Omit<HTMLAttributes<HTMLDivElement>, 'title'> {
  title?: ReactNode;
  subtitle?: ReactNode;
  titleMeta?: ReactNode;
  extra?: ReactNode;
  variant?: 'default' | 'flush';
  className?: string;
}

export function Card({ title, subtitle, titleMeta, extra, variant = 'default', children, className, ...props }: PropsWithChildren<CardProps>) {
  const cardClassName = [
    'card',
    variant === 'flush' ? 'card-flush' : '',
    className,
  ].filter(Boolean).join(' ');
  const hasHeading = title || subtitle || titleMeta;

  return (
    <div className={cardClassName} {...props}>
      {(hasHeading || extra) && (
        <div className="card-header">
          {hasHeading && (
            <div className="keeper-card-heading">
              {(title || titleMeta) && (
                <div className="keeper-card-title-track">
                  {title && <h3 className="keeper-card-title">{title}</h3>}
                  {titleMeta && <div className="keeper-card-title-meta">{titleMeta}</div>}
                </div>
              )}
              {subtitle && <p className="keeper-card-subtitle">{subtitle}</p>}
            </div>
          )}
          {extra && <div className="keeper-card-actions">{extra}</div>}
        </div>
      )}
      {children}
    </div>
  );
}
