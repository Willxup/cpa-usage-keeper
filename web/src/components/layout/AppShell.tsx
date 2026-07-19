import type { ReactNode } from 'react';
import { ShellContent } from './ShellContent';
import { ShellHeader } from './ShellHeader';
import { ShellNavigation } from './ShellNavigation';
import styles from './AppShell.module.scss';

const joinClassNames = (...classNames: Array<string | false | null | undefined>) => (
  classNames.filter(Boolean).join(' ')
);

export type AppShellVariant = 'authenticated' | 'viewer' | 'guest' | 'embed';
export type AppShellSticky = 'always' | 'desktop' | 'static';
export type AppShellNavMode = 'sidebar' | 'drawer' | 'none';
export type AppShellDensity = 'standard' | 'compact';

export type AppShellSlots = {
  brand: ReactNode;
  navigation?: ReactNode;
  headerTitle: ReactNode;
  headerSubtitle?: ReactNode;
  headerLeading?: ReactNode;
  headerActions?: ReactNode;
  headerUtility?: ReactNode;
  sidebarUtility?: ReactNode;
  toolbar?: ReactNode;
  content: ReactNode;
  footer?: ReactNode;
};

export type AppShellNav = {
  mode: AppShellNavMode;
};

export type AppShellProps = {
  variant: AppShellVariant;
  slots: AppShellSlots;
  sticky?: AppShellSticky;
  nav?: AppShellNav;
  density?: AppShellDensity;
  className?: string;
};

const DEFAULT_STICKY: Record<AppShellVariant, AppShellSticky> = {
  authenticated: 'always',
  viewer: 'desktop',
  guest: 'static',
  embed: 'static',
};

const DEFAULT_NAV_MODE: Record<AppShellVariant, AppShellNavMode> = {
  authenticated: 'sidebar',
  viewer: 'none',
  guest: 'none',
  embed: 'none',
};

const DEFAULT_DENSITY: Record<AppShellVariant, AppShellDensity> = {
  authenticated: 'standard',
  viewer: 'standard',
  guest: 'standard',
  embed: 'compact',
};

export function AppShell({ variant, slots, sticky, nav, density, className }: AppShellProps) {
  const stickyMode = sticky ?? DEFAULT_STICKY[variant];
  const navMode = nav?.mode ?? DEFAULT_NAV_MODE[variant];
  const densityMode = density ?? DEFAULT_DENSITY[variant];
  const hasSidebarNavigation = navMode === 'sidebar' && Boolean(slots.navigation);
  const chromeHidden = variant === 'embed';
  const brandInHeader = !hasSidebarNavigation && !chromeHidden;

  return (
    <div
      className={joinClassNames(styles.appShell, className)}
      data-shell="app"
      data-variant={variant}
      data-density={densityMode}
      data-sticky={stickyMode}
      data-nav={navMode}
    >
      {hasSidebarNavigation && (
        <ShellNavigation
          brand={slots.brand}
          navigation={slots.navigation}
          utility={slots.sidebarUtility ?? slots.headerUtility}
        />
      )}
      <div className={styles.appShellMain} data-shell-region="main">
        {!chromeHidden && (
          <ShellHeader
            sticky={stickyMode}
            brand={brandInHeader ? slots.brand : undefined}
            title={slots.headerTitle}
            subtitle={slots.headerSubtitle}
            leading={slots.headerLeading}
            actions={slots.headerActions}
            utility={slots.headerUtility}
          />
        )}
        <ShellContent toolbar={slots.toolbar} footer={slots.footer}>
          {slots.content}
        </ShellContent>
      </div>
    </div>
  );
}
