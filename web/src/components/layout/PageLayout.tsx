import type { ComponentProps, HTMLAttributes, ReactNode } from 'react';
import { Layout } from 'antd';
import styles from './PageLayout.module.scss';

const joinClassNames = (...classNames: Array<string | false | null | undefined>) => (
  classNames.filter(Boolean).join(' ')
);

export type PageHeaderStickyMode = 'always' | 'desktop' | 'none';

export type PageHeaderProps = Omit<ComponentProps<typeof Layout.Header>, 'children'> & {
  children: ReactNode;
  sticky?: PageHeaderStickyMode;
};

export function PageHeader({ children, className, sticky = 'always', ...props }: PageHeaderProps) {
  return (
    <Layout.Header
      {...props}
      className={joinClassNames(
        styles.pageHeader,
        sticky === 'always' && styles.stickyAlways,
        sticky === 'desktop' && styles.stickyDesktop,
        className,
      )}
      data-sticky={sticky}
    >
      <div className={styles.pageHeaderInner}>{children}</div>
    </Layout.Header>
  );
}

export type PageContentProps = Omit<ComponentProps<typeof Layout.Content>, 'children'> & {
  children: ReactNode;
  containerClassName?: string;
};

export function PageContent({ children, className, containerClassName, ...props }: PageContentProps) {
  return (
    <Layout.Content {...props} className={joinClassNames(styles.pageContent, className)}>
      <div className={joinClassNames(styles.pageContentInner, containerClassName)}>{children}</div>
    </Layout.Content>
  );
}

export type PageTitleProps = HTMLAttributes<HTMLHeadingElement> & {
  children: ReactNode;
};

export function PageTitle({ children, className, ...props }: PageTitleProps) {
  return (
    <h1 {...props} className={joinClassNames(styles.pageTitle, className)}>
      {children}
    </h1>
  );
}

export type PageToolbarProps = HTMLAttributes<HTMLDivElement> & {
  leading?: ReactNode;
  actions?: ReactNode;
};

export function PageToolbar({ leading, actions, children, className, ...props }: PageToolbarProps) {
  return (
    <div
      {...props}
      className={joinClassNames(styles.pageToolbar, !leading && styles.actionsOnly, className)}
    >
      {leading && <div className={styles.pageToolbarLeading}>{leading}</div>}
      {actions && <div className={styles.pageToolbarActions}>{actions}</div>}
      {children}
    </div>
  );
}
