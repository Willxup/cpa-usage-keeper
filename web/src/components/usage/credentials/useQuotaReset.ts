import { useCallback, useState } from 'react'
import i18n from '@/i18n'
import { resetUsageQuota } from '@/lib/api'
import type { UsageQuotaResetResponse } from '@/lib/types'

export interface CredentialResetState { quotaResetting?: boolean }
export function useQuotaReset({ refreshQuotaForAuthIndex, onNotice }: {
  refreshQuotaForAuthIndex: (authIndex: string) => Promise<void>
  onNotice?: (kind: 'success' | 'info' | 'error', message: string) => void
}) {
  const [quotaResetStateByAuthIndex, setQuotaResetStateByAuthIndex] = useState<Record<string, CredentialResetState>>({})
  const resetQuotaForAuthIndex = useCallback(async (authIndex: string, grantId?: string, organizationId?: string) => {
    setQuotaResetStateByAuthIndex((current) => ({
      ...current,
      [authIndex]: { quotaResetting: true },
    }))
    try {
      const outcome = await runQuotaResetForAuthIndex(authIndex, {
        resetUsageQuota: grantId ? (value) => resetUsageQuota(value, undefined, grantId, organizationId) : resetUsageQuota,
        refreshQuotaForAuthIndex,
        grantId,
      })
      setQuotaResetStateByAuthIndex((current) => ({
        ...current,
        [authIndex]: { quotaResetting: false },
      }))
      if (outcome.kind === 'error') {
        onNotice?.('error', outcome.message)
      } else if (outcome.kind === 'warning') {
        onNotice?.('info', outcome.message)
      }
      return outcome.result
    } catch {
      setQuotaResetStateByAuthIndex((current) => ({
        ...current,
        [authIndex]: { quotaResetting: false },
      }))
      onNotice?.('error', quotaResetDisplayError())
    }
  }, [onNotice, refreshQuotaForAuthIndex])

  return { quotaResetStateByAuthIndex, resetQuotaForAuthIndex }
}

export type QuotaResetOutcome = (
  | { kind: 'success' }
  | { kind: 'warning'; message: string }
  | { kind: 'error'; message: string }
) & { result?: UsageQuotaResetResponse }

export async function runQuotaResetForAuthIndex(
  authIndex: string,
  deps: {
    resetUsageQuota: (authIndex: string) => Promise<UsageQuotaResetResponse>
    refreshQuotaForAuthIndex: (authIndex: string) => Promise<void>
    grantId?: string
  },
): Promise<QuotaResetOutcome> {
  let result: UsageQuotaResetResponse
  try {
    // 后端在官方重置后恢复 CPA 路由；只有官方重置失败才中止额度刷新。
    result = await deps.resetUsageQuota(authIndex)
  } catch {
    if (deps.grantId) return { kind: 'warning', message: i18n.t('usage_stats.claude_reset_unknown'), result: { authIndex, code: 'unknown' } }
    return {
      kind: 'error',
      message: quotaResetDisplayError(),
    }
  }

  const response = deps.grantId ? { result } : {}
  if (deps.grantId && result.code !== 'reset' && result.code !== 'already_used') {
    return { kind: 'warning', message: i18n.t(`usage_stats.claude_reset_${claudeResetResultKey(result.code)}`), ...response }
  }

  try {
    // reset 成功后复用现有单行刷新，让缓存继续以官方刷新结果为准；刷新失败走原有行内错误链路。
    await deps.refreshQuotaForAuthIndex(authIndex)
  } catch {
    // reset 已成功消费官方次数，后续刷新失败不影响本次 reset 的成功提示。
  }
  if (result.recoveryFailed) {
    return { kind: 'warning', message: i18n.t('usage_stats.credentials_quota_reset_recovery_failed'), ...response }
  }
  return { kind: 'success', ...response }
}

export function claudeResetResultKey(code?: string): string {
  return ['not_limited', 'cooldown', 'ineligible', 'unavailable', 'rate_limited', 'auth_error', 'status_unavailable', 'unknown'].includes(code ?? '') ? code! : 'unavailable'
}

export function quotaResetDisplayError(): string {
  return i18n.t('usage_stats.credentials_quota_reset_failed', { defaultValue: 'Quota reset failed. Please try again later.' })
}

