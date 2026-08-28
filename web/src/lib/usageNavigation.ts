export const USAGE_TAB_OPTIONS = [
  'overview',
  'analysis',
  'ranking',
  'events',
  'auth-files',
  'ai-provider',
  'settings',
] as const;

export type UsageTab = (typeof USAGE_TAB_OPTIONS)[number];
export const DEFAULT_USAGE_TAB: UsageTab = 'overview';

const USAGE_TAB_PATHS: Record<UsageTab, string> = {
  overview: '/overview',
  analysis: '/analysis',
  ranking: '/ranking',
  events: '/request-events',
  'auth-files': '/auth-files',
  'ai-provider': '/ai-provider',
  settings: '/settings',
};

const USAGE_PATH_TABS = new Map<string, UsageTab>(
  Object.entries(USAGE_TAB_PATHS).map(([tab, path]) => [path, tab as UsageTab]),
);

export const normalizeUsageTabValue = (value: unknown): UsageTab | null => {
  if (value === 'credentials') return 'auth-files';
  return typeof value === 'string' && USAGE_TAB_OPTIONS.includes(value as UsageTab)
    ? value as UsageTab
    : null;
};

export const getUsageTabPath = (tab: UsageTab): string => USAGE_TAB_PATHS[tab];

export const resolveUsageTabFromPath = (path: string): UsageTab | null => {
  // 仅精确匹配静态白名单，不兼容尾斜杠或嵌套路径，避免扩大服务端路由边界。
  return USAGE_PATH_TABS.get(path) ?? null;
};

export const stripAppBasePath = (pathname: string, basePath: string | undefined): string | null => {
  if (!basePath || basePath === '/' || basePath === '__APP_BASE_PATH__') return pathname || '/';

  const normalizedBase = basePath.endsWith('/') ? basePath.slice(0, -1) : basePath;
  if (pathname === normalizedBase) return '/';
  if (!pathname.startsWith(`${normalizedBase}/`)) return null;

  return pathname.slice(normalizedBase.length) || '/';
};

export const resolveInitialUsageTab = (
  pathname: string,
  basePath: string | undefined,
  storedTab: unknown,
): UsageTab => {
  const currentPath = stripAppBasePath(pathname, basePath);
  const routedTab = currentPath === null ? null : resolveUsageTabFromPath(currentPath);
  return routedTab ?? normalizeUsageTabValue(storedTab) ?? DEFAULT_USAGE_TAB;
};

type UsageNavigationEvent = Pick<
  MouseEvent,
  'altKey' | 'button' | 'ctrlKey' | 'defaultPrevented' | 'metaKey' | 'shiftKey'
>;

export const shouldHandleUsageNavigation = (event: UsageNavigationEvent): boolean => (
  !event.defaultPrevented
  && event.button === 0
  && !event.altKey
  && !event.ctrlKey
  && !event.metaKey
  && !event.shiftKey
);

type UsageTabKeyEvent = Pick<KeyboardEvent, 'key' | 'preventDefault'>;

export const handleUsageTabKeyActivation = (
  event: UsageTabKeyEvent,
  tab: UsageTab,
  activate: (tab: UsageTab) => void,
): void => {
  // 链接原生支持 Enter；这里只恢复原 button 页签已有的 Space 激活行为。
  if (event.key !== ' ') return;
  event.preventDefault();
  activate(tab);
};
