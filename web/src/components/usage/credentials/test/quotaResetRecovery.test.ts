import { describe, expect, it, vi } from 'vitest'
import i18n from '@/i18n'
import { runQuotaResetForAuthIndex } from '../useCredentialsTabData'

describe('quota reset recovery', () => {
  it.each([false, true])('reports partial success and refreshes quota even if refresh fails: %s', async (refreshFails) => {
    const resetUsageQuota = vi.fn().mockResolvedValue({ authIndex: 'auth-1', code: 'reset', windowsReset: 2, recoveryFailed: true })
    const refreshQuotaForAuthIndex = vi.fn(async () => {
      if (refreshFails) throw new Error('refresh failed')
    })

    const outcome = await runQuotaResetForAuthIndex('auth-1', { resetUsageQuota, refreshQuotaForAuthIndex })

    expect(outcome).toEqual({
      kind: 'warning',
      message: 'Quota was reset, but CPA account recovery failed. Recover the account in CPA; do not reset quota again.',
    })
    expect(resetUsageQuota).toHaveBeenCalledTimes(1)
    expect(refreshQuotaForAuthIndex).toHaveBeenCalledWith('auth-1')
  })

  it.each(['en', 'zh', 'zh-TW'])('provides a recovery warning in %s', (language) => {
    expect(i18n.getResource(language, 'translation', 'usage_stats.credentials_quota_reset_recovery_failed')).toEqual(expect.any(String))
  })
})
