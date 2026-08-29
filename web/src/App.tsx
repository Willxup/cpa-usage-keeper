import { useCallback, useEffect, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import './index.css';
import './App.css';
import './embed/cpamcEmbed.css';
import { ApiError, appPath, clearEmbedSessionToken, getSession, login, loginWithCPAAPIKey } from './lib/api';
import type { AuthRole, AuthSessionAPIKeySummary } from './lib/types';
import { AppFooter } from './components/AppFooter';
import { isKeyViewerPath, type KeyViewerPath } from './features/key-viewer';
import { KeyAnalysisPage } from './pages/KeyAnalysisPage';
import { KeyOverviewPage } from './pages/KeyOverviewPage';
import { LoginPage } from './pages/LoginPage';
import { UsagePage } from './pages/UsagePage';
import { cpamcEmbedSearch, isCPAMCEmbed, notifyCPAMCEmbedReady } from './embed/cpamcEmbed';
import { getUsageTabPath, resolveUsageTabFromPath, stripAppBasePath } from './lib/usageNavigation';
import { useUsageStatsStore } from './stores/useUsageStatsStore';

type AuthState = 'checking' | 'authenticated' | 'unauthenticated';
const getInitialKeyViewerPath = (): KeyViewerPath => {
  if (typeof window === 'undefined') return '/key-overview';
  const currentPath = stripAppBasePath(window.location.pathname, window.__APP_BASE_PATH__) ?? '/';
  return isKeyViewerPath(currentPath) ? currentPath : '/key-overview';
};

export const getRoleHomePath = (role: AuthRole): '/' | '/key-overview' => (
  role === 'api_key_viewer' ? '/key-overview' : '/'
);

export const getRoleTargetPath = (
  role: AuthRole,
  currentPath: string,
  isEmbeddedInCPAMC = false,
): string => {
  // 路径白名单与会话角色共同决定落点；未知路径只回到该角色自己的首页。
  if (role === 'api_key_viewer') {
    return isKeyViewerPath(currentPath) ? currentPath : '/key-overview';
  }
  if (currentPath === '/') return '/';

  const usageTab = resolveUsageTabFromPath(currentPath);
  if (!usageTab || (isEmbeddedInCPAMC && usageTab === 'ranking')) return '/';
  return getUsageTabPath(usageTab);
};

export const shouldNormalizeRolePath = (
  role: AuthRole,
  currentPath: string,
  isEmbeddedInCPAMC = false,
): boolean => currentPath !== getRoleTargetPath(role, currentPath, isEmbeddedInCPAMC);

function App() {
  const { t } = useTranslation();
  const [authState, setAuthState] = useState<AuthState>('checking');
  const [authRole, setAuthRole] = useState<AuthRole | null>(null);
  const [sessionAPIKey, setSessionAPIKey] = useState<AuthSessionAPIKeySummary | undefined>();
  const [keyViewerPath, setKeyViewerPath] = useState<KeyViewerPath>(getInitialKeyViewerPath);
  const [adminLoginError, setAdminLoginError] = useState('');
  const [apiKeyLoginError, setAPIKeyLoginError] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const clearUsageStats = useUsageStatsStore((state) => state.clearUsageStats);
  const isEmbeddedInCPAMC = isCPAMCEmbed();

  const clearSession = useCallback(() => {
    clearEmbedSessionToken();
    clearUsageStats();
    setAuthState('unauthenticated');
    setAuthRole(null);
    setSessionAPIKey(undefined);
  }, [clearUsageStats]);

  const applySession = useCallback((session: Awaited<ReturnType<typeof getSession>>) => {
    if (!session.authenticated) {
      clearSession();
      return;
    }
    setAuthState('authenticated');
    setAuthRole(session.role ?? 'admin');
    setSessionAPIKey(session.api_key);
  }, [clearSession]);

  const loadSession = useCallback(async () => {
    const session = await getSession();
    applySession(session);
    return session;
  }, [applySession]);

  useEffect(() => {
    void loadSession().catch(() => {
      clearSession();
    });
  }, [clearSession, loadSession]);

  useEffect(() => {
    notifyCPAMCEmbedReady();
  }, []);

  useEffect(() => {
    if (authState !== 'authenticated' || !authRole) return;
    const strippedPath = stripAppBasePath(window.location.pathname, window.__APP_BASE_PATH__);
    const targetPath = getRoleTargetPath(authRole, strippedPath ?? '/', isEmbeddedInCPAMC);
    if (authRole === 'api_key_viewer') {
      setKeyViewerPath(targetPath as KeyViewerPath);
    }
    if (strippedPath === targetPath) return;
    window.history.replaceState(null, '', appPath(targetPath) + cpamcEmbedSearch());
  }, [authRole, authState, isEmbeddedInCPAMC]);

  const handlePasswordLogin = useCallback(async (password: string) => {
    setSubmitting(true);
    setAdminLoginError('');
    try {
      await login(password);
      const session = await loadSession();
      if (!session.authenticated) {
        setAdminLoginError(t('auth.login_failed'));
        clearSession();
        return;
      }
      const currentPath = stripAppBasePath(window.location.pathname, window.__APP_BASE_PATH__) ?? '/';
      const targetPath = getRoleTargetPath(session.role ?? 'admin', currentPath, isEmbeddedInCPAMC);
      window.history.replaceState(null, '', appPath(targetPath) + cpamcEmbedSearch());
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) {
        setAdminLoginError(t('auth.invalid_password'));
      } else {
        setAdminLoginError(t('auth.login_failed'));
      }
      clearSession();
    } finally {
      setSubmitting(false);
    }
  }, [clearSession, isEmbeddedInCPAMC, loadSession, t]);

  const handleAPIKeyLogin = useCallback(async (apiKey: string) => {
    setSubmitting(true);
    setAPIKeyLoginError('');
    try {
      await loginWithCPAAPIKey(apiKey);
      const session = await loadSession();
      if (!session.authenticated || session.role !== 'api_key_viewer') {
        setAPIKeyLoginError(t('auth.api_key_login_failed'));
        clearSession();
        return;
      }
      const currentPath = stripAppBasePath(window.location.pathname, window.__APP_BASE_PATH__) ?? '/';
      const targetPath = getRoleTargetPath(session.role, currentPath, isEmbeddedInCPAMC) as KeyViewerPath;
      setKeyViewerPath(targetPath);
      window.history.replaceState(null, '', appPath(targetPath) + cpamcEmbedSearch());
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) {
        setAPIKeyLoginError(t('auth.invalid_api_key'));
      } else if (error instanceof ApiError && error.status === 429) {
        setAPIKeyLoginError(t('auth.login_rate_limited'));
      } else {
        setAPIKeyLoginError(t('auth.api_key_login_failed'));
      }
      clearSession();
    } finally {
      setSubmitting(false);
    }
  }, [clearSession, isEmbeddedInCPAMC, loadSession, t]);

  const handleKeyViewerNavigate = useCallback((path: KeyViewerPath) => {
    if (path === keyViewerPath) return;
    window.history.replaceState(null, '', appPath(path) + cpamcEmbedSearch());
    setKeyViewerPath(path);
  }, [keyViewerPath]);

  let page: ReactNode;
  if (authState === 'checking') {
    page = <div className="app-checking" aria-busy="true" />;
  } else if (authState === 'unauthenticated') {
    page = <LoginPage loading={submitting} adminError={adminLoginError} apiKeyError={apiKeyLoginError} onPasswordSubmit={handlePasswordLogin} onAPIKeySubmit={handleAPIKeyLogin} />;
  } else if (authRole === 'api_key_viewer') {
    page = keyViewerPath === '/key-analysis'
      ? <KeyAnalysisPage apiKey={sessionAPIKey} onNavigate={handleKeyViewerNavigate} onAuthRequired={clearSession} />
      : <KeyOverviewPage apiKey={sessionAPIKey} onNavigate={handleKeyViewerNavigate} onAuthRequired={clearSession} />;
  } else {
    page = <UsagePage onAuthRequired={clearSession} />;
  }

  return (
    <div className="app-frame" data-embed={isEmbeddedInCPAMC ? 'cpamc' : undefined}>
      <main className="app-main">{page}</main>
      <AppFooter loadVersion={authState === 'authenticated'} />
    </div>
  );
}

export default App;
