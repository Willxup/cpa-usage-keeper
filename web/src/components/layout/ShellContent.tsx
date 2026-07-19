import type { HTMLAttributes, ReactNode } from 'react';
import styles from './AppShell.module.scss';

const joinClassNames = (...classNames: Array<string | false | null | undefined>) => (
  classNames.filter(Boolean).join(' ')
);

export type ShellContentProps = HTMLAttributes<HTMLElement> & {
  toolbar?: ReactNode;
  children: ReactNode;
  footer?: ReactNode;
};

export function ShellContent({ toolbar, children, footer, className, ...props }: ShellContentProps) {
  return (
    <main {...props} className={joinClassNames(styles.shellContent, className)} data-shell-slot="content">
      <div className={styles.shellContentInner}>
        {toolbar && <div className={styles.shellContentToolbar}>{toolbar}</div>}
        {children}
        {footer && <footer className={styles.shellContentFooter}>{footer}</footer>}
      </div>
    </main>
  );
}
