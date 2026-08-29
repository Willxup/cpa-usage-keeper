import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { ApiError, fetchKeyAnalysis, fetchKeyAnalysisLatency, isUsageRangeBoundsConflict } from '@/lib/api';
import type { AnalysisLatencyDiagnostics, AnalysisResponse, AuthSessionAPIKeySummary, UsageCustomRange, UsageTimeRange } from '@/lib/types';
import { AnalysisPanel, TimeRangeControl } from '@/components/usage';
import { MainActionButton } from '@/components/ui/MainActionButton';
import { IconRefreshCw } from '@/components/ui/icons';
import { KeyViewerShell } from '@/features/key-viewer/KeyViewerShell';
import type { KeyViewerPath } from '@/features/key-viewer/navigation';
import { useMediaQuery } from '@/hooks/useMediaQuery';
import { useThemeStore } from '@/stores';
import {
  clampStoredUsageRangeStateToCurrentBounds,
  parseStoredUsageRangeState,
  resolveUsageRangeRecoveryTimeZone,
  serializeUsageRangeState,
  type StoredUsageRangeState,
} from '@/utils/usage/customRange';
import { buildUsageRangeQuery } from '@/utils/usage/rangeQuery';
import styles from '@/features/key-viewer/KeyViewerShell.module.scss';

const KEY_ANALYSIS_RANGE_STORAGE_KEY = 'cli-proxy-key-analysis-range-v1';
const DEFAULT_TIME_RANGE: UsageTimeRange = 'today';

const loadTimeRange = (): StoredUsageRangeState => {
  try {
    if (typeof localStorage === 'undefined') return { range: DEFAULT_TIME_RANGE };
    return parseStoredUsageRangeState(localStorage.getItem(KEY_ANALYSIS_RANGE_STORAGE_KEY), { nowMs: Date.now() });
  } catch {
    return { range: DEFAULT_TIME_RANGE };
  }
};

export interface KeyAnalysisPageProps {
  apiKey?: AuthSessionAPIKeySummary;
  onNavigate: (path: KeyViewerPath) => void;
  onAuthRequired?: () => void;
}

export function KeyAnalysisPage({ apiKey, onNavigate, onAuthRequired }: KeyAnalysisPageProps) {
  const { t } = useTranslation();
  const isMobile = useMediaQuery('(max-width: 768px)');
  const resolvedTheme = useThemeStore((state) => state.resolvedTheme);
  const isDark = resolvedTheme === 'dark';
  const [timeRangeState, setTimeRangeState] = useState<StoredUsageRangeState>(loadTimeRange);
  const { range: timeRange, customRange } = timeRangeState;
  const [analysis, setAnalysis] = useState<AnalysisResponse | null>(null);
  const [analysisLoading, setAnalysisLoading] = useState(false);
  const [analysisError, setAnalysisError] = useState('');
  const [latency, setLatency] = useState<AnalysisLatencyDiagnostics | null>(null);
  const [latencyLoading, setLatencyLoading] = useState(false);
  const [latencyError, setLatencyError] = useState('');
  const [manualRefreshLoading, setManualRefreshLoading] = useState(false);
  const requestControllerRef = useRef<AbortController | null>(null);
  const analysisTimeZoneRef = useRef(timeRangeState.timeZone);
  const usageRangeQuery = useMemo(() => buildUsageRangeQuery({
    range: timeRange,
    customUnit: customRange?.unit,
    customStart: customRange?.start,
    customEnd: customRange?.end,
  }), [customRange?.end, customRange?.start, customRange?.unit, timeRange]);
  const rangeTimeZone = analysis?.timezone ?? timeRangeState.timeZone;

  const recoverRangeBoundsConflict = useCallback((error: unknown) => {
    if (!isUsageRangeBoundsConflict(error)) return false;
    const timeZone = resolveUsageRangeRecoveryTimeZone(timeRangeState, analysisTimeZoneRef.current)?.trim();
    if (!timeZone) return false;
    const nextState = clampStoredUsageRangeStateToCurrentBounds(timeRangeState, {
      nowMs: Date.now(),
      timeZone,
    });
    if (nextState === timeRangeState) return false;
    setTimeRangeState(nextState);
    return true;
  }, [timeRangeState]);

  const handleTimeRangeChange = useCallback((range: UsageTimeRange, nextCustomRange?: UsageCustomRange) => {
    if (range === 'custom' && nextCustomRange) {
      setTimeRangeState({ range, customRange: nextCustomRange, timeZone: rangeTimeZone });
      return;
    }
    setTimeRangeState((current) => ({ ...current, range }));
  }, [rangeTimeZone]);

  const loadAnalysis = useCallback(async () => {
    if (!usageRangeQuery.valid) return;
    requestControllerRef.current?.abort();
    const controller = new AbortController();
    requestControllerRef.current = controller;
    setAnalysisLoading(true);
    setAnalysisError('');
    setAnalysis(null);
    setLatencyLoading(true);
    setLatencyError('');
    setLatency(null);

    // 主 Analysis 与 Latency 同时加载，但分别更新状态，避免一个慢请求阻塞另一块内容。
    const coreRequest = fetchKeyAnalysis(usageRangeQuery, controller.signal).then((response) => {
      if (requestControllerRef.current !== controller) return;
      // 项目时区只作为后续 409 恢复依据，不参与当前请求 callback 身份，避免响应触发重复加载。
      analysisTimeZoneRef.current = response.timezone;
      setAnalysis(response);
      setAnalysisLoading(false);
    }, (error: unknown) => {
      if (controller.signal.aborted || requestControllerRef.current !== controller) return;
      setAnalysisLoading(false);
      if (recoverRangeBoundsConflict(error)) return;
      if (error instanceof ApiError && error.status === 401) {
        onAuthRequired?.();
        return;
      }
      setAnalysisError('KEY_ANALYSIS_LOAD_FAILED');
    });
    const latencyRequest = fetchKeyAnalysisLatency(usageRangeQuery, controller.signal).then((response) => {
      if (requestControllerRef.current !== controller) return;
      setLatency(response);
      setLatencyLoading(false);
    }, (error: unknown) => {
      if (controller.signal.aborted || requestControllerRef.current !== controller) return;
      setLatencyLoading(false);
      if (recoverRangeBoundsConflict(error)) return;
      if (error instanceof ApiError && error.status === 401) {
        onAuthRequired?.();
        return;
      }
      setLatencyError('KEY_ANALYSIS_LATENCY_LOAD_FAILED');
    });
    await Promise.all([coreRequest, latencyRequest]);
    if (requestControllerRef.current === controller) {
      requestControllerRef.current = null;
    }
  }, [onAuthRequired, recoverRangeBoundsConflict, usageRangeQuery]);

  useEffect(() => {
    void loadAnalysis();
    return () => {
      requestControllerRef.current?.abort();
      requestControllerRef.current = null;
    };
  }, [loadAnalysis]);

  useEffect(() => {
    try {
      localStorage.setItem(KEY_ANALYSIS_RANGE_STORAGE_KEY, serializeUsageRangeState(timeRangeState));
    } catch {
      // 忽略浏览器存储不可用；页面仍使用当前内存状态。
    }
  }, [timeRangeState]);

  const handleManualRefresh = useCallback(async () => {
    if (manualRefreshLoading) return;
    setManualRefreshLoading(true);
    try {
      await loadAnalysis();
    } finally {
      setManualRefreshLoading(false);
    }
  }, [loadAnalysis, manualRefreshLoading]);

  const displayAnalysisError = analysisError ? t('key_analysis.load_failed') : '';
  const displayLatencyError = latencyError ? t('key_analysis.latency_load_failed') : '';
  const toolbar = (
    <>
      <div className={styles.usageFilterBar}>
        <TimeRangeControl
          value={timeRange}
          customRange={customRange}
          timeZone={rangeTimeZone}
          onChange={handleTimeRangeChange}
          ariaLabel={t('usage_stats.range_filter')}
        />
      </div>
      <div className={styles.usageRefreshSlot}>
        <div className={styles.usageFilterActions}>
          <MainActionButton
            type="button"
            shellClassName={styles.refreshMainActionShell}
            className={styles.refreshMainActionButton}
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
      activePage="analysis"
      apiKey={apiKey}
      loading={analysisLoading && !analysis}
      toolbar={toolbar}
      onNavigate={onNavigate}
      onAuthRequired={onAuthRequired}
    >
      {displayAnalysisError && (
        <div className={`${styles.errorBox} ${styles.errorBoxWithAction}`} role="alert">
          <span>{displayAnalysisError}</span>
          <button type="button" className={styles.errorRetryButton} onClick={() => void handleManualRefresh()}>
            {t('common.retry')}
          </button>
        </div>
      )}
      <AnalysisPanel
        analysis={analysis}
        loading={analysisLoading}
        latencyDiagnostics={latency}
        latencyLoading={latencyLoading}
        latencyError={displayLatencyError}
        isDark={isDark}
        isMobile={isMobile}
        compositionDimensions={['api_key', 'model']}
      />
    </KeyViewerShell>
  );
}
