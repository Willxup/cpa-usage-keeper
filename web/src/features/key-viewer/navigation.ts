export type KeyViewerPage = 'overview' | 'realtime' | 'analysis' | 'ranking' | 'quota';
export type KeyViewerPath = '/key-overview' | '/key-realtime' | '/key-analysis' | '/key-ranking' | '/key-quota';

export const KEY_VIEWER_PAGE_PATHS: Record<KeyViewerPage, KeyViewerPath> = {
  overview: '/key-overview',
  realtime: '/key-realtime',
  analysis: '/key-analysis',
  ranking: '/key-ranking',
  quota: '/key-quota',
};

const KEY_VIEWER_PATHS = new Set<KeyViewerPath>(Object.values(KEY_VIEWER_PAGE_PATHS));

export const isKeyViewerPath = (path: string): path is KeyViewerPath => KEY_VIEWER_PATHS.has(path as KeyViewerPath);
