import type { HTMLAttributes, ReactNode } from 'react';
import styles from './AppShell.module.scss';

const joinClassNames = (...classNames: Array<string | false | null | undefined>) => (
  classNames.filter(Boolean).join(' ')
);

export type ShellNavigationProps = HTMLAttributes<HTMLElement> & {
  brand: ReactNode;
  navigation?: ReactNode;
  utility?: ReactNode;
};

export function ShellNavigation({ brand, navigation, utility, className, ...props }: ShellNavigationProps) {
  return (
    <aside {...props} className={joinClassNames(styles.shellNavigation, className)} data-shell-slot="navigation">
      <div className={styles.shellNavigationBrand}>{brand}</div>
      {navigation && <div className={styles.shellNavigationMenu}>{navigation}</div>}
      {utility && <div className={styles.shellNavigationUtility}>{utility}</div>}
    </aside>
  );
}
