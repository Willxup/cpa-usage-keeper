import { useCallback, useEffect, useRef, useState } from 'react'
import {
  fetchRelayPlatformAssignments,
  fetchRelayPlatformOverrides,
  fetchRelayProviderUsage,
  updateRelayPlatformOverrides,
} from '@/lib/api'
import type { RelayPlatformAssignment, RelayUsageItem } from '@/lib/types'

// useRelayProviderUsage 管理 AI Provider 凭证的中转商用量查询状态。
// assignments 是轻量的平台判定（只匹配不查接口），usage 是实际调用中转商接口的结果。
// 两者分离：进入页面只拉 assignments 显示平台 badge，点刷新才触发 usage 查询（避免无谓的外部请求）。

export interface UseRelayProviderUsage {
  assignments: Map<string, RelayPlatformAssignment>
  usage: Map<string, RelayUsageItem>
  loadingUsage: boolean
  error?: string
  refreshUsage: (identityIds?: string[]) => Promise<void>
  setPlatformOverride: (identityId: string, platform: string) => Promise<void>
}

export function useRelayProviderUsage(identityIds: string[]): UseRelayProviderUsage {
  const [assignments, setAssignments] = useState<Map<string, RelayPlatformAssignment>>(new Map())
  const [usage, setUsage] = useState<Map<string, RelayUsageItem>>(new Map())
  const [overrides, setOverrides] = useState<Record<string, string>>({})
  const [loadingUsage, setLoadingUsage] = useState(false)
  const [error, setError] = useState<string | undefined>()

  const seqRef = useRef(0)
  const idsRef = useRef(identityIds)
  idsRef.current = identityIds

  const loadAssignments = useCallback(async (ids: string[]) => {
    if (ids.length === 0) return
    const seq = ++seqRef.current
    try {
      const [overridesResp, assignmentsResp] = await Promise.all([
        fetchRelayPlatformOverrides(),
        fetchRelayPlatformAssignments(ids),
      ])
      if (seq !== seqRef.current) return
      setOverrides(overridesResp.overrides ?? {})
      const map = new Map<string, RelayPlatformAssignment>()
      for (const item of assignmentsResp.assignments ?? []) {
        map.set(item.identity_id, item)
      }
      setAssignments(map)
      setError(undefined)
    } catch (err) {
      if (seq !== seqRef.current) return
      setError(err instanceof Error ? err.message : 'Failed to load relay platform info')
    }
  }, [])

  const refreshUsage = useCallback(async (ids?: string[]) => {
    const targetIds = ids ?? idsRef.current
    if (targetIds.length === 0) return
    setLoadingUsage(true)
    try {
      const resp = await fetchRelayProviderUsage(targetIds)
      setUsage((prev) => {
        const map = new Map(prev)
        for (const item of resp.items ?? []) {
          map.set(item.identity_id, item)
        }
        return map
      })
      setError(undefined)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load relay usage')
    } finally {
      setLoadingUsage(false)
    }
  }, [])

  const setPlatformOverride = useCallback(async (identityId: string, platform: string) => {
    const previous = overrides
    // 'auto' 表示删除手动覆盖，回退到按域名自动识别。
    const next: Record<string, string> = { ...overrides }
    if (platform === 'auto') {
      delete next[identityId]
    } else {
      next[identityId] = platform
    }
    setOverrides(next) // 乐观更新
    try {
      const resp = await updateRelayPlatformOverrides(next)
      setOverrides(resp.overrides ?? {})
      // 覆盖变更后平台判定可能变化，重新拉 assignments。
      await loadAssignments(idsRef.current)
    } catch (err) {
      setOverrides(previous) // 回滚
      setError(err instanceof Error ? err.message : 'Failed to update platform override')
    }
  }, [overrides, loadAssignments])

  // identityIds 变化时加载 assignments + overrides，并默认拉一次用量，让数据进页面即展示。
  // 用 join 串作依赖，避免数组引用抖动导致重复请求；usage 只对匹配中转商的凭证真正请求外部接口。
  const idsKey = identityIds.join(',')
  useEffect(() => {
    const ids = idsKey ? idsKey.split(',') : []
    void loadAssignments(ids)
    void refreshUsage(ids)
  }, [idsKey, loadAssignments, refreshUsage])

  return {
    assignments,
    usage,
    loadingUsage,
    error,
    refreshUsage,
    setPlatformOverride,
  }
}
