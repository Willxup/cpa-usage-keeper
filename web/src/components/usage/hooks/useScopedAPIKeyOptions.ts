import { useCallback, useEffect, useRef, useState } from 'react';
import { ApiError, fetchCpaApiKeyOptions, type CpaApiKeyOptionsRequest } from '@/lib/api';
import type { CpaApiKeyOption } from '@/lib/types';

// 候选与身份名册独立；过期只标记，不清空上次成功结果。
export function useScopedAPIKeyOptions(
  request: CpaApiKeyOptionsRequest | null,
  onOptions: (options: CpaApiKeyOption[]) => void,
  onAuthRequired?: () => void,
  onRangeBoundsConflict?: (error: unknown) => boolean,
) {
  const controllerRef = useRef<AbortController | null>(null);
  const revisionRef = useRef(0);
  const loadedRevisionRef = useRef(-1);
  const [state, setState] = useState<{ options: CpaApiKeyOption[]; hasResult: boolean; status: 'loading' | 'ready' | 'error' }>({ options: [], hasResult: false, status: 'ready' });
  const invalidate = useCallback(() => { revisionRef.current += 1; }, []);
  // 手动刷新强制请求；打开菜单则由 ensureFresh 判断是否需要更新。
  const refresh = useCallback(async () => {
    controllerRef.current?.abort();
    controllerRef.current = null;
    if (!request) return;
    const controller = new AbortController();
    controllerRef.current = controller;
    const revision = revisionRef.current;
    loadedRevisionRef.current = -1;
    setState((current) => ({ ...current, status: current.hasResult ? 'ready' : 'loading' }));
    try {
      const response = await fetchCpaApiKeyOptions(controller.signal, request);
      if (controller.signal.aborted || controllerRef.current !== controller) return;
      const options = response.options ?? [];
      onOptions(options);
      loadedRevisionRef.current = revision;
      setState({ options, hasResult: true, status: 'ready' });
    } catch (error) {
      if (controller.signal.aborted || controllerRef.current !== controller) return;
      setState((current) => ({ ...current, status: 'error' }));
      if (error instanceof ApiError && error.status === 401) onAuthRequired?.();
      else onRangeBoundsConflict?.(error);
    } finally {
      if (controllerRef.current === controller) controllerRef.current = null;
    }
  }, [request, onOptions, onAuthRequired, onRangeBoundsConflict]);
  useEffect(() => {
    invalidate();
    return () => { controllerRef.current?.abort(); controllerRef.current = null; };
  }, [invalidate, refresh]);
  const ensureFresh = useCallback(async () => {
    if (controllerRef.current || loadedRevisionRef.current === revisionRef.current) return;
    await refresh();
  }, [refresh]);
  return { options: state.options, status: request ? state.status : 'ready', refresh, ensureFresh, invalidate };
}
