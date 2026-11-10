import { useCallback, useEffect, useRef, useState } from 'react'
import { ApiError, fetchAvailableModels } from '@/lib/api'
import { normalizeCatalog, type CatalogModel } from './modelCatalogUtils'

export interface ModelCatalogData {
  models: CatalogModel[]
  loading: boolean
  loaded: boolean
  error: boolean
  refresh: () => Promise<void>
}

export function useModelCatalogData({ enabled = true, onAuthRequired }: { enabled?: boolean; onAuthRequired?: () => void } = {}): ModelCatalogData {
  const [models, setModels] = useState<CatalogModel[]>([])
  const [loading, setLoading] = useState(true)
  const [loaded, setLoaded] = useState(false)
  const [error, setError] = useState(false)
  const loadedRef = useRef(false)
  const request = useRef<AbortController | null>(null)
  const onAuthRequiredRef = useRef(onAuthRequired)

  useEffect(() => { onAuthRequiredRef.current = onAuthRequired }, [onAuthRequired])

  const refresh = useCallback(async () => {
    request.current?.abort()
    const controller = new AbortController()
    request.current = controller
    setLoading(true)
    setError(false)
    try {
      const result = await fetchAvailableModels(controller.signal)
      if (controller.signal.aborted) return
      setModels(normalizeCatalog(result.data))
      loadedRef.current = true
      setLoaded(true)
    } catch (failure) {
      if (controller.signal.aborted) return
      if (failure instanceof ApiError && failure.status === 401) onAuthRequiredRef.current?.()
      setError(true)
    } finally {
      if (!controller.signal.aborted) setLoading(false)
    }
  }, [])

  // 切走时取消未完成的读取；成功加载过的目录留在页面内，切回时不再做首次读取。
  useEffect(() => {
    if (enabled && !loadedRef.current) void refresh()
    else if (!enabled && loadedRef.current) setLoading(false)
    return () => request.current?.abort()
  }, [enabled, refresh])

  return { models, loading, loaded, error, refresh }
}
