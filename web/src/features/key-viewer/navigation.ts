export type KeyViewerPage = 'overview' | 'realtime' | 'analysis' | 'events' | 'credentials';
export type KeyViewerPath = '/key-overview' | '/key-realtime' | '/key-analysis' | '/key-events' | '/key-credentials';

export const KEY_VIEWER_PAGE_PATHS: Record<KeyViewerPage, KeyViewerPath> = {
  overview: '/key-overview',
  realtime: '/key-realtime',
  analysis: '/key-analysis',
  events: '/key-events',
  credentials: '/key-credentials',
};

const KEY_VIEWER_PATHS = new Set<KeyViewerPath>(Object.values(KEY_VIEWER_PAGE_PATHS));

export const isKeyViewerPath = (path: string): path is KeyViewerPath => KEY_VIEWER_PATHS.has(path as KeyViewerPath);
