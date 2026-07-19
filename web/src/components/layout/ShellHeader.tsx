import type { HTMLAttributes, ReactNode } from 'react';
import styles from './AppShell.module.scss';

const joinClassNames = (...classNames: Array<string | false | null | undefined>) => (
  classNames.filter(Boolean).join(' ')
);

export type ShellHeaderSticky = 'always' | 'desktop' | 'static';

export type ShellHeaderProps = Omit<HTMLAttributes<HTMLElement>, 'title'> & {
  brand?: ReactNode;
  title: ReactNode;
  subtitle?: ReactNode;
  leading?: ReactNode;
  actions?: ReactNode;
  utility?: ReactNode;
  sticky?: ShellHeaderSticky;
};

export function ShellHeader({
  brand,
  title,
  subtitle,
  leading,
  actions,
  utility,
  sticky = 'always',
  className,
  ...props
}: ShellHeaderProps) {
  return (
    <header
      {...props}
      className={joinClassNames(
        styles.shellHeader,
        sticky === 'always' && styles.shellHeaderStickyAlways,
        sticky === 'desktop' && styles.shellHeaderStickyDesktop,
        className,
      )}
      data-shell-slot="header"
      data-sticky={sticky}
    >
      <div className={styles.shellHeaderInner}>
        {leading && <div className={styles.shellHeaderLeading}>{leading}</div>}
        {brand && <div className={styles.shellHeaderBrand}>{brand}</div>}
        <div className={styles.shellHeaderHeading}>
          <h1 className={styles.shellHeaderTitle}>{title}</h1>
          {subtitle && <p className={styles.shellHeaderSubtitle}>{subtitle}</p>}
        </div>
        {actions && <div className={styles.shellHeaderActions}>{actions}</div>}
        {utility && <div className={styles.shellHeaderUtility}>{utility}</div>}
      </div>
    </header>
  );
}
