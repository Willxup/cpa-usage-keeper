import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { ApiError, fetchKeyActivity, fetchUsageActivity } from '@/lib/api';
import type { UsageActivityRequest, UsageActivityResponse, UsageTimeRange } from '@/lib/types';

export interface UseUsageActivityDataOptions {
  viewer: 'admin' | 'key';
  request: UsageActivityRequest;
  apiKeyId?: string;
  enabled?: boolean;
  onAuthRequired?: () => void;
}

export interface UseUsageActivityDataReturn {
  activity: UsageActivityResponse | null;
  loading: boolean;
  error: string;
  requestIdentity: string;
  loadActivity: (options?: LoadUsageActivityOptions) => Promise<void>;
}

export interface LoadUsageActivityOptions {
  skipIfInFlight?: boolean;
}

interface ActiveActivityRequest {
  key: string;
  controller: AbortController;
  promise: Promise<void>;
}

export function useUsageActivityData({
  viewer,
  request,
  apiKeyId,
  enabled = true,
  onAuthRequired,
}: UseUsageActivityDataOptions): UseUsageActivityDataReturn {
  const normalizedAPIKeyID = viewer === 'admin' ? apiKeyId?.trim() ?? '' : '';
  const requestWindow = 'window' in request ? request.window : undefined;
  const requestRange = 'range' in request ? request.range : undefined;
  const requestUnit = 'range' in request ? request.unit : undefined;
  const requestStart = 'range' in request ? request.start : undefined;
  const requestEnd = 'range' in request ? request.end : undefined;
  const normalizedRequest = useMemo<UsageActivityRequest>(() => (
    requestWindow
      ? { window: requestWindow }
      : {
          range: requestRange as UsageTimeRange,
          unit: requestUnit,
          start: requestStart,
          end: requestEnd,
        }
  ), [requestEnd, requestRange, requestStart, requestUnit, requestWindow]);
  const queryKey = useMemo(
    () => `${viewer}:${normalizedAPIKeyID}:${requestWindow ?? ''}:${requestRange ?? ''}:${requestUnit ?? ''}:${requestStart ?? ''}:${requestEnd ?? ''}`,
    [normalizedAPIKeyID, requestEnd, requestRange, requestStart, requestUnit, requestWindow, viewer],
  );
  const queryScope = `${viewer}:${normalizedAPIKeyID}`;
  const activeRequestRef = useRef<ActiveActivityRequest | null>(null);
  const [response, setResponse] = useState<UsageActivityResponse | null>(null);
  const [responseQueryKey, setResponseQueryKey] = useState('');
  const [responseQueryScope, setResponseQueryScope] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [errorQueryKey, setErrorQueryKey] = useState('');

  const loadActivity = useCallback((options: LoadUsageActivityOptions = {}) => {
    const currentRequest = activeRequestRef.current;
    if (options.skipIfInFlight && currentRequest?.key === queryKey) {
      return currentRequest.promise;
    }
    activeRequestRef.current?.controller.abort();
    const controller = new AbortController();
    const activeRequest: ActiveActivityRequest = {
      key: queryKey,
      controller,
      promise: Promise.resolve(),
    };
    activeRequestRef.current = activeRequest;
    setLoading(true);
    setError('');
    setErrorQueryKey('');
    activeRequest.promise = (async () => {
      try {
        const next = viewer === 'key'
          ? await fetchKeyActivity({ request: normalizedRequest, signal: controller.signal })
          : await fetchUsageActivity({ request: normalizedRequest, apiKeyId: normalizedAPIKeyID, signal: controller.signal });
        if (activeRequestRef.current !== activeRequest) return;
        setResponse(next);
        setResponseQueryKey(queryKey);
        setResponseQueryScope(queryScope);
        setLoading(false);
      } catch (nextError) {
        if (controller.signal.aborted || activeRequestRef.current !== activeRequest) return;
        let message = 'ACTIVITY_LOAD_FAILED';
        if (nextError instanceof ApiError && nextError.status === 401) {
          message = 'AUTH_REQUIRED';
          onAuthRequired?.();
        } else if (viewer === 'key' && nextError instanceof ApiError && nextError.status === 429) {
          message = 'KEY_ACTIVITY_RATE_LIMITED';
        }
        setLoading(false);
        setError(message);
        setErrorQueryKey(queryKey);
      } finally {
        if (activeRequestRef.current === activeRequest) {
          activeRequestRef.current = null;
        }
      }
    })();
    return activeRequest.promise;
  }, [normalizedAPIKeyID, normalizedRequest, onAuthRequired, queryKey, queryScope, viewer]);

  useEffect(() => {
    if (!enabled) return;
    void loadActivity();
    return () => {
      activeRequestRef.current?.controller.abort();
      activeRequestRef.current = null;
    };
  }, [enabled, loadActivity]);

  // 同一 viewer/API Key 范围切换窗口时保留旧 payload，成功后原位替换；失败或跨 scope 时停止复用。
  const displayActivity = responseQueryKey === queryKey
    ? response
    : responseQueryScope === queryScope && errorQueryKey !== queryKey
      ? response
      : null;

  return {
    activity: displayActivity,
    loading,
    error: errorQueryKey === queryKey ? error : '',
    requestIdentity: queryKey,
    loadActivity,
  };
}
