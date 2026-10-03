export type KeyViewerPage = 'overview' | 'realtime' | 'analysis' | 'ranking' | 'quota' | 'events' | 'auth-files' | 'ai-provider';
export type KeyViewerPath = '/key-overview' | '/key-realtime' | '/key-analysis' | '/key-ranking' | '/key-quota' | '/key-events' | '/key-auth-files' | '/key-ai-provider';

export const KEY_VIEWER_PAGE_PATHS: Record<KeyViewerPage, KeyViewerPath> = {
  overview: '/key-overview',
  realtime: '/key-realtime',
  analysis: '/key-analysis',
  ranking: '/key-ranking',
  quota: '/key-quota', events: '/key-events', 'auth-files': '/key-auth-files', 'ai-provider': '/key-ai-provider',
};

const KEY_VIEWER_PATHS = new Set<KeyViewerPath>(Object.entries(KEY_VIEWER_PAGE_PATHS).filter(([page])=>!['events','auth-files','ai-provider'].includes(page)).map(([,path])=>path));

export const isKeyViewerPath = (path: string): path is KeyViewerPath => KEY_VIEWER_PATHS.has(path as KeyViewerPath);

export const READ_ONLY_PAGE_PATHS: Record<KeyViewerPage, string> = {
  overview: '/read-only', realtime: '/read-only/realtime', analysis: '/read-only/analysis',
  quota: '/read-only/quota', ranking: '/read-only/ranking', events: '/read-only/events', 'auth-files': '/read-only/auth-files', 'ai-provider': '/read-only/ai-provider',
};
export function getReadOnlyPage(path: string): KeyViewerPage | undefined {
  return (['overview', 'realtime', 'analysis', 'quota', 'ranking', 'events', 'auth-files', 'ai-provider'] as const).find(page => READ_ONLY_PAGE_PATHS[page] === path);
}
