export type KeyViewerPage = 'overview' | 'analysis';
export type KeyViewerPath = '/key-overview' | '/key-analysis';

export const KEY_VIEWER_PAGE_PATHS: Record<KeyViewerPage, KeyViewerPath> = {
  overview: '/key-overview',
  analysis: '/key-analysis',
};

const KEY_VIEWER_PATHS = new Set<KeyViewerPath>(Object.values(KEY_VIEWER_PAGE_PATHS));

export const isKeyViewerPath = (path: string): path is KeyViewerPath => KEY_VIEWER_PATHS.has(path as KeyViewerPath);
