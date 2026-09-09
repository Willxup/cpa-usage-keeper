import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { MainActionButton } from '@/components/ui/MainActionButton';
import { IconRefreshCw } from '@/components/ui/icons';
import { KeyViewerShell } from '@/features/key-viewer/KeyViewerShell';
import type { KeyViewerPath } from '@/features/key-viewer/navigation';
import {
  fetchKeyLocalRankingLeaderboard,
  fetchKeyRankingLeaderboard,
  RankingApiError,
} from '@/features/ranking/api';
import { RankingLeaderboardResults } from '@/features/ranking/components/RankingLeaderboardResults';
import { RankingScopeSwitch } from '@/features/ranking/components/RankingScopeSwitch';
import { RankingMetricSelect, RankingToolbar } from '@/features/ranking/components/RankingToolbar';
import type {
  RankingLeaderboardResponse,
  RankingMetric,
  RankingPeriod,
  RankingScope,
} from '@/features/ranking/types';
import type { AuthSessionAPIKeySummary } from '@/lib/types';
import shellStyles from '@/features/key-viewer/KeyViewerShell.module.scss';
import rankingStyles from '@/features/ranking/RankingPage.module.scss';

const KEY_RANKING_POLL_INTERVAL_MS = 60_000;

const isAbortError = (error: unknown) => error instanceof DOMException && error.name === 'AbortError';

const formatDateTime = (value: string, language: string): string => {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat(language, {
    dateStyle: 'medium',
    timeStyle: 'short',
    hour12: false,
  }).format(date);
};

export interface KeyRankingPageProps {
  apiKey?: AuthSessionAPIKeySummary;
  onNavigate: (path: KeyViewerPath) => void;
  onAuthRequired?: () => void;
}

export function KeyRankingPage({ apiKey, onNavigate, onAuthRequired }: KeyRankingPageProps) {
  const { t, i18n } = useTranslation();
  const localRankingEnabled = apiKey?.local_ranking_enabled === true;
  const [scope, setScope] = useState<RankingScope>('community');
  const effectiveScope: RankingScope = localRankingEnabled && scope === 'local' ? 'local' : 'community';
  const [period, setPeriod] = useState<RankingPeriod>('today');
  const [metric, setMetric] = useState<RankingMetric>('overall');
  const [leaderboard, setLeaderboard] = useState<RankingLeaderboardResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [manualRefreshLoading, setManualRefreshLoading] = useState(false);
  const requestControllerRef = useRef<AbortController | null>(null);

  // 筛选提交时同步废弃旧响应，避免被动 Effect 启动前用新标签渲染旧榜单。
  const beginSelectionChange = useCallback(() => {
    requestControllerRef.current?.abort();
    requestControllerRef.current = null;
    setLeaderboard(null);
    setError(null);
    setLoading(true);
  }, []);
  const handleScopeChange = useCallback((nextScope: RankingScope) => {
    if (nextScope === effectiveScope) return;
    beginSelectionChange();
    setScope(nextScope);
  }, [beginSelectionChange, effectiveScope]);
  const handlePeriodChange = useCallback((nextPeriod: RankingPeriod) => {
    if (nextPeriod === period) return;
    beginSelectionChange();
    setPeriod(nextPeriod);
  }, [beginSelectionChange, period]);
  const handleMetricChange = useCallback((nextMetric: RankingMetric) => {
    if (nextMetric === metric) return;
    beginSelectionChange();
    setMetric(nextMetric);
  }, [beginSelectionChange, metric]);

  const loadLeaderboard = useCallback(async ({ preserveCurrent = false }: { preserveCurrent?: boolean } = {}) => {
    requestControllerRef.current?.abort();
    const controller = new AbortController();
    requestControllerRef.current = controller;
    setLoading(true);
    setError(null);
    if (!preserveCurrent) setLeaderboard(null);

    try {
      const nextLeaderboard = effectiveScope === 'local'
        ? await fetchKeyLocalRankingLeaderboard(period, metric, controller.signal)
        : await fetchKeyRankingLeaderboard(period, metric, controller.signal);
      if (requestControllerRef.current !== controller) return;
      setLeaderboard(nextLeaderboard);
    } catch (loadError) {
      if (controller.signal.aborted || requestControllerRef.current !== controller || isAbortError(loadError)) return;
      if (loadError instanceof RankingApiError && loadError.status === 401) {
        onAuthRequired?.();
        return;
      }
      // 中心明确撤下当前周期时不能继续展示旧榜单；临时刷新失败仍保留已有内容。
      if (
        loadError instanceof RankingApiError
        && loadError.status === 404
        && loadError.code === 'ranking_center_leaderboard_unavailable'
      ) {
        setLeaderboard(null);
      }
      setError(loadError);
    } finally {
      if (requestControllerRef.current === controller) {
        requestControllerRef.current = null;
        setLoading(false);
      }
    }
  }, [effectiveScope, metric, onAuthRequired, period]);

  useEffect(() => {
    void loadLeaderboard();
    return () => {
      requestControllerRef.current?.abort();
      requestControllerRef.current = null;
    };
  }, [loadLeaderboard]);

  useEffect(() => {
    const interval = window.setInterval(() => {
      if (document.visibilityState === 'hidden') return;
      void loadLeaderboard({ preserveCurrent: true });
    }, KEY_RANKING_POLL_INTERVAL_MS);
    return () => window.clearInterval(interval);
  }, [loadLeaderboard]);

  const handleManualRefresh = useCallback(async () => {
    if (manualRefreshLoading) return;
    setManualRefreshLoading(true);
    try {
      await loadLeaderboard({ preserveCurrent: true });
    } finally {
      setManualRefreshLoading(false);
    }
  }, [loadLeaderboard, manualRefreshLoading]);

  const rows = useMemo(() => leaderboard?.entries.slice(0, 100) ?? [], [leaderboard]);
  const toolbar = (
    <>
      {localRankingEnabled ? (
        <div className={shellStyles.usageFilterBar}>
          <RankingScopeSwitch value={effectiveScope} onChange={handleScopeChange} />
        </div>
      ) : null}
      <div className={shellStyles.usageRefreshSlot}>
        <div className={shellStyles.usageFilterActions}>
          <MainActionButton
            type="button"
            shellClassName={shellStyles.refreshMainActionShell}
            className={shellStyles.refreshMainActionButton}
            onClick={() => void handleManualRefresh()}
            disabled={manualRefreshLoading}
            loading={manualRefreshLoading}
          >
            {manualRefreshLoading ? t('common.loading') : (
              <>
                <IconRefreshCw size={14} />
                <span>{t('usage_stats.refresh')}</span>
              </>
            )}
          </MainActionButton>
        </div>
      </div>
    </>
  );

  return (
    <KeyViewerShell
      activePage="ranking"
      apiKey={apiKey}
      loading={loading && !leaderboard}
      toolbar={toolbar}
      onNavigate={onNavigate}
      onAuthRequired={onAuthRequired}
    >
      <section className={rankingStyles.page} aria-label={t('ranking.title')}>
        <article className={`card ${rankingStyles.leaderboardCard}`.trim()} aria-busy={loading}>
          <header className={rankingStyles.leaderboardHeader}>
            <div className={rankingStyles.leaderboardTitle} data-ranking-header-title>
              <div className="keeper-card-title-track">
                <div
                  className={`${rankingStyles.metricTitleHeading} keeper-card-title`}
                  role="heading"
                  aria-level={2}
                  data-ranking-metric-title
                >
                  <RankingMetricSelect metric={metric} onMetricChange={handleMetricChange} />
                </div>
                <div className={rankingStyles.leaderboardHeaderToolbar} data-ranking-header-toolbar>
                  <RankingToolbar period={period} onPeriodChange={handlePeriodChange} />
                </div>
              </div>
              {leaderboard ? (
                <div className={rankingStyles.boardMeta}>
                  {leaderboard.stale ? (
                    <span className={rankingStyles.staleBadge} data-ranking-stale>
                      {t('ranking.stale')} · {leaderboard.period_key}
                    </span>
                  ) : null}
                  <span>{t('ranking.updated_at', { time: formatDateTime(leaderboard.generated_at, i18n.language) })}</span>
                </div>
              ) : null}
            </div>
          </header>

          {error ? (
            <div className={`${shellStyles.errorBox} ${shellStyles.errorBoxWithAction}`} role="alert">
              <span>{t('ranking.error_generic')}</span>
              <button type="button" className={shellStyles.errorRetryButton} onClick={() => void handleManualRefresh()}>
                {t('common.retry')}
              </button>
            </div>
          ) : null}
          {(!error || leaderboard) && (
            rows.length === 0 && !loading ? (
              <div className={rankingStyles.emptyState}>
                <strong>{t(effectiveScope === 'local' ? 'ranking.local_empty_title' : 'ranking.empty_title')}</strong>
                <p>{t(effectiveScope === 'local' ? 'ranking.local_empty_description' : 'ranking.empty_description')}</p>
              </div>
            ) : rows.length > 0 ? (
              <RankingLeaderboardResults scope={effectiveScope} metric={metric} entries={rows} />
            ) : null
          )}
        </article>
      </section>
    </KeyViewerShell>
  );
}
