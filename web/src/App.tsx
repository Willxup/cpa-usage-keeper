import { lazy, Suspense, useCallback, useEffect, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import './index.css';
import './App.css';
import { ApiError, appPath, clearEmbedSessionToken, getSession, login, loginWithCPAAPIKey } from './lib/api';
import type { AuthRole, AuthSessionAPIKeySummary } from './lib/types';
import type { AppShellVariant } from './components/layout';
import { LoginPage } from './pages/LoginPage';
import { cpamcEmbedSearch, isCPAMCEmbed, notifyCPAMCEmbedReady } from './embed/cpamcEmbed';
import { EmbedContext } from './embed/EmbedContext';
import { useUsageStatsStore } from './stores/useUsageStatsStore';

type AuthState = 'checking' | 'authenticated' | 'unauthenticated';

const KeyOverviewPage = lazy(() => import('./pages/KeyOverviewPage').then((module) => ({
  default: module.KeyOverviewPage
})));
const UsagePage = lazy(() => import('./pages/UsagePage').then((module) => ({
  default: module.UsagePage
})));

const protectedPageFallback = <div className="app-checking" aria-busy="true" />;

export const getRoleHomePath = (role: AuthRole): '/' | '/key-overview' => (
  role === 'api_key_viewer' ? '/key-overview' : '/'
);

const stripBasePath = (pathname: string, basePath: string | undefined): string => {
  if (!basePath || basePath === '/' || basePath === '__APP_BASE_PATH__') return pathname || '/';
  const normalizedBase = basePath.endsWith('/') ? basePath.slice(0, -1) : basePath;
  if (!pathname.startsWith(normalizedBase)) return pathname || '/';
  const stripped = pathname.slice(normalizedBase.length);
  return stripped || '/';
};

export const shouldNormalizeRolePath = (role: AuthRole, currentPath: string): boolean => currentPath !== getRoleHomePath(role);

function App() {
  const { t } = useTranslation();
  const [authState, setAuthState] = useState<AuthState>('checking');
  const [authRole, setAuthRole] = useState<AuthRole | null>(null);
  const [sessionAPIKey, setSessionAPIKey] = useState<AuthSessionAPIKeySummary | undefined>();
  const [adminLoginError, setAdminLoginError] = useState('');
  const [apiKeyLoginError, setAPIKeyLoginError] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const clearUsageStats = useUsageStatsStore((state) => state.clearUsageStats);
  const isEmbeddedInCPAMC = isCPAMCEmbed();
  const shellVariant: AppShellVariant = isEmbeddedInCPAMC
    ? 'embed'
    : authState === 'unauthenticated'
      ? 'guest'
      : authRole === 'api_key_viewer'
        ? 'viewer'
        : 'authenticated';

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
    const currentPath = stripBasePath(window.location.pathname, window.__APP_BASE_PATH__);
    if (!shouldNormalizeRolePath(authRole, currentPath)) return;
    window.history.replaceState(null, '', appPath(getRoleHomePath(authRole)) + cpamcEmbedSearch());
  }, [authRole, authState]);

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
      window.history.replaceState(null, '', appPath('/') + cpamcEmbedSearch());
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
  }, [clearSession, loadSession, t]);

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
      window.history.replaceState(null, '', appPath('/key-overview') + cpamcEmbedSearch());
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
  }, [clearSession, loadSession, t]);

  let page: ReactNode;
  if (authState === 'checking') {
    page = <div className="app-checking" aria-busy="true" />;
  } else if (authState === 'unauthenticated') {
    page = <LoginPage loading={submitting} adminError={adminLoginError} apiKeyError={apiKeyLoginError} onPasswordSubmit={handlePasswordLogin} onAPIKeySubmit={handleAPIKeyLogin} />;
  } else if (authRole === 'api_key_viewer') {
    page = (
      <Suspense fallback={protectedPageFallback}>
        <KeyOverviewPage apiKey={sessionAPIKey} onAuthRequired={clearSession} />
      </Suspense>
    );
  } else {
    page = (
      <Suspense fallback={protectedPageFallback}>
        <UsagePage onAuthRequired={clearSession} />
      </Suspense>
    );
  }

  return (
    <EmbedContext.Provider value={isEmbeddedInCPAMC}>
      <div className="app-shell-host" data-shell-variant={shellVariant}>
        <main className="app-main">{page}</main>
      </div>
    </EmbedContext.Provider>
  );
}

export default App;
