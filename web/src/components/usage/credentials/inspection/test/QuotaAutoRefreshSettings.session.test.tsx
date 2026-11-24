// @vitest-environment happy-dom
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { expect, it, vi } from 'vitest'
import { fetchQuotaAutoRefreshSettings, updateQuotaAutoRefreshSettings } from '@/lib/api'
import { QuotaAutoRefreshSettingsModal } from '../QuotaAutoRefreshSettingsModal'

vi.mock('@/lib/api', async (original) => ({
  ...await original<typeof import('@/lib/api')>(),
  fetchQuotaAutoRefreshSettings: vi.fn(),
  updateQuotaAutoRefreshSettings: vi.fn(),
}))
const translate = (key: string) => key
vi.mock('react-i18next', async (original) => ({
  ...await original<typeof import('react-i18next')>(),
  useTranslation: () => ({ t: translate }),
}))
globalThis.IS_REACT_ACT_ENVIRONMENT = true

it('allows saving disabled defaults after settings loading fails', async () => {
  vi.mocked(fetchQuotaAutoRefreshSettings).mockRejectedValue(new Error('offline'))
  vi.mocked(updateQuotaAutoRefreshSettings).mockResolvedValue({ enabled: false, schedule: null })
  const node = document.createElement('div')
  document.body.appendChild(node)
  const root = createRoot(node)
  try {
    await act(async () => root.render(<QuotaAutoRefreshSettingsModal open onClose={() => undefined} />))
    expect(document.body.textContent).toContain('offline')
    const save = document.querySelector<HTMLButtonElement>('button[aria-label="usage_stats.credentials_auto_refresh_save"]')!
    expect(save.disabled).toBe(false)
    await act(async () => save.click())
    expect(updateQuotaAutoRefreshSettings).toHaveBeenCalledWith({ enabled: false, schedule: null })
    expect(document.body.textContent).not.toContain('offline')
  } finally {
    await act(async () => root.unmount())
    node.remove()
    vi.clearAllMocks()
  }
})
