import React, { type PropsWithChildren, type ReactNode, type HTMLAttributes } from 'react';

interface CardProps extends Omit<HTMLAttributes<HTMLDivElement>, 'title'> {
  title?: ReactNode;
  extra?: ReactNode;
  className?: string;
}

export function Card({ title, extra, children, className, ...props }: PropsWithChildren<CardProps>) {
  return (
    <div className={className ? `card ${className}` : 'card'} {...props}>
      {(title || extra) && (
        <div className="card-header">
          <div className="title">{title}</div>
          {extra}
        </div>
      )}
      {children}
    </div>
  );
}
