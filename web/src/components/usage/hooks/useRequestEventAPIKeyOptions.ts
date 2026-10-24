import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { ApiError, fetchUsageEventAPIKeyFilterOptions, type UsageEventAPIKeyFilters } from '@/lib/api';
import type { CpaApiKeyOption, UsageRangeRequest } from '@/lib/types';

export interface RequestEventAPIKeyOptionsParams {
  request: UsageRangeRequest;
  filters: UsageEventAPIKeyFilters;
  enabled: boolean;
  onAuthRequired?: () => void;
  onRangeBoundsConflict?: (error: unknown) => boolean;
  onOptionsLoaded?: (options: CpaApiKeyOption[]) => void;
}

type OptionsState = {
  scope: string;
  options: CpaApiKeyOption[];
  status: 'idle' | 'loading' | 'ready' | 'error';
};

export function useRequestEventAPIKeyOptions({ request, filters, enabled, onAuthRequired, onRangeBoundsConflict, onOptionsLoaded }: RequestEventAPIKeyOptionsParams) {
  const { range, unit, start, end } = request;
  const { model, source, result } = filters;
  const query = useMemo(() => ({ range, unit, start, end }), [range, unit, start, end]);
  const queryFilters = useMemo(() => ({ model, source, result }), [model, source, result]);
  const scope = JSON.stringify([query, queryFilters]);
  const [state, setState] = useState<OptionsState>({ scope: '', options: [], status: 'idle' });
  const activeRequest = useRef<AbortController | null>(null);
  const requested = useRef(false);

  const refresh = useCallback(async () => {
    if (!enabled) return;
    requested.current = true;
    activeRequest.current?.abort();
    const controller = new AbortController();
    activeRequest.current = controller;
    setState({ scope, options: [], status: 'loading' });
    try {
      const response = await fetchUsageEventAPIKeyFilterOptions(query, controller.signal, queryFilters);
      if (controller.signal.aborted || activeRequest.current !== controller) return;
      onOptionsLoaded?.(response.options ?? []);
      setState({ scope, options: response.options ?? [], status: 'ready' });
    } catch (error) {
      if (controller.signal.aborted || activeRequest.current !== controller) return;
      setState({ scope, options: [], status: 'error' });
      if (onRangeBoundsConflict?.(error)) return;
      if (error instanceof ApiError && error.status === 401) onAuthRequired?.();
    } finally {
      if (activeRequest.current === controller) activeRequest.current = null;
    }
  }, [enabled, onAuthRequired, onRangeBoundsConflict, onOptionsLoaded, query, queryFilters, scope]);

  useEffect(() => {
    // Once opened, changing the other filters must replace any visible candidates immediately.
    if (enabled && requested.current) void refresh();
    return () => {
      activeRequest.current?.abort();
      activeRequest.current = null;
    };
  }, [enabled, refresh]);

  const matching = enabled && state.scope === scope;
  return {
    options: matching ? state.options : [],
    status: matching ? state.status : 'idle',
    refresh,
  };
}
