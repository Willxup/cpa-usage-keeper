import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { ApiError, fetchKeyActivity, fetchUsageActivity } from '@/lib/api';
import type { UsageActivityResponse, UsageRangeRequest } from '@/lib/types';

export interface UseUsageActivityDataOptions {
  viewer: 'admin' | 'key';
  request: UsageRangeRequest;
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
  const normalizedRequest = useMemo<UsageRangeRequest>(() => ({
    range: request.range,
    unit: request.unit,
    start: request.start,
    end: request.end,
  }), [request.end, request.range, request.start, request.unit]);
  const queryKey = useMemo(
    () => `${viewer}:${normalizedAPIKeyID}:${normalizedRequest.range}:${normalizedRequest.unit ?? ''}:${normalizedRequest.start ?? ''}:${normalizedRequest.end ?? ''}`,
    [normalizedAPIKeyID, normalizedRequest.end, normalizedRequest.range, normalizedRequest.start, normalizedRequest.unit, viewer],
  );
  const activeRequestRef = useRef<ActiveActivityRequest | null>(null);
  const [response, setResponse] = useState<UsageActivityResponse | null>(null);
  const [responseQueryKey, setResponseQueryKey] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

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
    activeRequest.promise = (async () => {
      try {
        const next = viewer === 'key'
          ? await fetchKeyActivity({ request: normalizedRequest, signal: controller.signal })
          : await fetchUsageActivity({ request: normalizedRequest, apiKeyId: normalizedAPIKeyID, signal: controller.signal });
        if (activeRequestRef.current !== activeRequest) return;
        setResponse(next);
        setResponseQueryKey(queryKey);
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
      } finally {
        if (activeRequestRef.current === activeRequest) {
          activeRequestRef.current = null;
        }
      }
    })();
    return activeRequest.promise;
  }, [normalizedAPIKeyID, normalizedRequest, onAuthRequired, queryKey, viewer]);

  useEffect(() => {
    if (!enabled) return;
    void loadActivity();
    return () => {
      activeRequestRef.current?.controller.abort();
      activeRequestRef.current = null;
    };
  }, [enabled, loadActivity]);

  return {
    activity: responseQueryKey === queryKey ? response : null,
    loading,
    error,
    requestIdentity: queryKey,
    loadActivity,
  };
}
