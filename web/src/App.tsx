import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import './index.css';
import { ApiError, appPath, getSession, login, loginWithCPAAPIKey } from './lib/api';
import type { AuthRole, AuthSessionAPIKeySummary } from './lib/types';
import { KeyOverviewPage } from './pages/KeyOverviewPage';
import { LoginPage } from './pages/LoginPage';
import { UsagePage } from './pages/UsagePage';
import { useUsageStatsStore } from './stores/useUsageStatsStore';

type AuthState = 'checking' | 'authenticated' | 'unauthenticated';

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

  const clearSession = useCallback(() => {
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
    if (authState !== 'authenticated' || !authRole) return;
    const currentPath = stripBasePath(window.location.pathname, window.__APP_BASE_PATH__);
    if (!shouldNormalizeRolePath(authRole, currentPath)) return;
    window.history.replaceState(null, '', appPath(getRoleHomePath(authRole)));
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
      window.history.replaceState(null, '', appPath('/'));
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
      window.history.replaceState(null, '', appPath('/key-overview'));
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

  if (authState === 'checking') {
    return <div style={{ minHeight: '100vh', background: 'var(--bg-secondary)' }} aria-busy="true" />;
  }

  if (authState === 'unauthenticated') {
    return <LoginPage loading={submitting} adminError={adminLoginError} apiKeyError={apiKeyLoginError} onPasswordSubmit={handlePasswordLogin} onAPIKeySubmit={handleAPIKeyLogin} />;
  }

  if (authRole === 'api_key_viewer') {
    return <KeyOverviewPage apiKey={sessionAPIKey} onAuthRequired={clearSession} />;
  }

  return <UsagePage onAuthRequired={clearSession} />;
}

export default App;
