// @vitest-environment happy-dom
import { act, useState } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fetchUsageQuotaInspectionStatus, setCredentialsDisabled } from '@/lib/api'
import type { UsageQuotaInspectionStatusResponse } from '@/lib/types'
import { useQuotaInspection } from '../../useQuotaInspection'
import { QuotaInspectionModal } from '../QuotaInspectionModal'

vi.mock('@/lib/api', async (original) => ({
  ...await original<typeof import('@/lib/api')>(),
  fetchUsageQuotaInspectionStatus: vi.fn(),
  setCredentialsDisabled: vi.fn(),
}))
vi.mock('react-i18next', async (original) => ({
  ...await original<typeof import('react-i18next')>(),
  useTranslation: () => ({ t: (key: string) => key }),
}))
globalThis.IS_REACT_ACT_ENVIRONMENT = true

const deferred = () => {
  let resolve!: () => void
  const promise = new Promise<void>((done) => { resolve = done })
  return { promise, resolve }
}
const status = (files: string[]): UsageQuotaInspectionStatusResponse => ({
  total: files.length, cached: files.length, running: false, completed: true, normal: 0,
  limit_reached: 0, unauthorized_401: files.length, payment_required_402: 0,
  unauthorized_401_402: files.length, other_failed: 0, unknown: 0,
  results: files.map((file, i) => ({ auth_index: `auth-${i}`, file_name: file, name: file, type: 'codex', status: 'unauthorized_401' })),
})
const button = (text: string) => [...document.querySelectorAll<HTMLButtonElement>('button')].find((item) => item.textContent === text)!

describe('invalid inspection account session', () => {
  let container: HTMLDivElement
  let root: Root
  beforeEach(() => { container = document.createElement('div'); document.body.appendChild(container); root = createRoot(container) })
  afterEach(async () => { await act(async () => root.unmount()); container.remove(); vi.clearAllMocks() })

  it('keeps its candidate snapshot and waits for both refreshes before closing', async () => {
    const write = deferred(), refresh = deferred(), after = deferred()
    vi.mocked(setCredentialsDisabled).mockReturnValue(write.promise as ReturnType<typeof setCredentialsDisabled>)
    const onRefreshStatus = vi.fn(() => refresh.promise)
    const onAfterInvalidAccountAction = vi.fn(() => after.promise)
    const onClose = vi.fn()
    const render = (files: string[]) => <QuotaInspectionModal open status={status(files)} loading={false} starting={false} error=""
      onClose={onClose} onStart={async () => undefined} onRefreshStatus={onRefreshStatus} onAfterInvalidAccountAction={onAfterInvalidAccountAction} />
    await act(async () => root.render(render(['a.json', 'b.json'])))
    await act(async () => button('usage_stats.credentials_inspection_disable_invalid').click())
    const boxes = [...document.querySelectorAll<HTMLInputElement>('input[type="checkbox"]')]
    await act(async () => boxes[0].click())
    await act(async () => root.render(render(['c.json'])))
    expect(document.body.textContent).toContain('a.json')
    expect(document.body.textContent).toContain('b.json')
    expect(document.querySelectorAll<HTMLInputElement>('input[type="checkbox"]')[0].checked).toBe(false)
    await act(async () => button('usage_stats.credentials_inspection_invalid_accounts_confirm').click())
    expect(setCredentialsDisabled).toHaveBeenCalledWith(['b.json'], true)
    expect(button('common.cancel').disabled).toBe(true)
    expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(2)
    await act(async () => write.resolve())
    expect(onRefreshStatus).toHaveBeenCalledOnce()
    expect(onAfterInvalidAccountAction).toHaveBeenCalledOnce()
    await act(async () => refresh.resolve())
    expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(2)
    await act(async () => after.resolve())
    expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(1)
    expect(onClose).not.toHaveBeenCalled()
  })

  it.each(['success', 'failure'] as const)('keeps the real session locked during a slow status refresh ending in %s', async (outcome) => {
    vi.useFakeTimers()
    try {
      const running = { ...status(['a.json', 'b.json', 'c.json']), total: 4, running: true, completed: false }
      const completed = { ...running, running: false, completed: true }
      let finishRefresh!: (value: UsageQuotaInspectionStatusResponse) => void
      let failRefresh!: (error: Error) => void
      const after = deferred()
      const onCompleted = vi.fn()
      vi.mocked(fetchUsageQuotaInspectionStatus).mockReset()
        .mockResolvedValueOnce(running)
        .mockImplementationOnce((signal) => new Promise((resolve, reject) => {
          finishRefresh = resolve
          failRefresh = reject
          signal?.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')))
        }))
        .mockResolvedValueOnce(running)
        .mockResolvedValueOnce(completed)
      vi.mocked(setCredentialsDisabled).mockResolvedValue({} as Awaited<ReturnType<typeof setCredentialsDisabled>>)
      function ConnectedInspection() {
        const inspection = useQuotaInspection({ enabled: true, onInspectionCompleted: onCompleted })
        return <QuotaInspectionModal open status={inspection.quotaInspectionStatus} loading={inspection.quotaInspectionLoading}
          starting={inspection.quotaInspectionStarting} error={inspection.quotaInspectionError} onClose={() => undefined}
          onStart={inspection.startQuotaInspection} onRefreshStatus={inspection.refreshQuotaInspectionStatus}
          onAfterInvalidAccountAction={() => after.promise} />
      }
      await act(async () => root.render(<ConnectedInspection />))
      await act(async () => button('usage_stats.credentials_inspection_disable_invalid').click())
      const boxes = [...document.querySelectorAll<HTMLInputElement>('input[type="checkbox"]')]
      await act(async () => boxes[0].click())
      await act(async () => button('usage_stats.credentials_inspection_invalid_accounts_confirm').click())
      expect(setCredentialsDisabled).toHaveBeenCalledWith(['b.json', 'c.json'], true)
      const signal = vi.mocked(fetchUsageQuotaInspectionStatus).mock.calls[1][0]!
      await act(async () => after.resolve())
      await act(async () => vi.advanceTimersByTimeAsync(3_000))
      expect(signal.aborted).toBe(false)
      expect(fetchUsageQuotaInspectionStatus).toHaveBeenCalledTimes(2)
      expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(2)
      expect(button('common.cancel').disabled).toBe(true)
      expect(button('usage_stats.credentials_inspection_disable_invalid').disabled).toBe(true)
      expect(document.querySelectorAll<HTMLButtonElement>('[role="dialog"] button[aria-label="common.close"]')[0].disabled).toBe(true)
      expect(document.querySelectorAll<HTMLInputElement>('input[type="checkbox"]')[0].checked).toBe(false)
      await act(async () => button('usage_stats.credentials_inspection_invalid_accounts_confirm').click())
      expect(setCredentialsDisabled).toHaveBeenCalledOnce()

      await act(async () => {
        if (outcome === 'success') finishRefresh(running)
        else failRefresh(new Error('temporary outage'))
      })
      expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(1)
      expect(button('usage_stats.credentials_inspection_disable_invalid').disabled).toBe(false)
      if (outcome === 'failure') expect(document.body.textContent).toContain('temporary outage')
      await act(async () => vi.advanceTimersByTimeAsync(3_000))
      expect(fetchUsageQuotaInspectionStatus).toHaveBeenCalledTimes(3)
      expect(document.body.textContent).not.toContain('temporary outage')
      await act(async () => vi.advanceTimersByTimeAsync(3_000))
      expect(fetchUsageQuotaInspectionStatus).toHaveBeenCalledTimes(4)
      expect(onCompleted).toHaveBeenCalledOnce()
    } finally {
      vi.useRealTimers()
    }
  })

  it('retains selection and error when a write fails', async () => {
    vi.mocked(setCredentialsDisabled).mockRejectedValue(new Error('write failed'))
    await act(async () => root.render(<QuotaInspectionModal open status={status(['a.json'])} loading={false} starting={false} error=""
      onClose={() => undefined} onStart={async () => undefined} onRefreshStatus={async () => undefined} />))
    await act(async () => button('usage_stats.credentials_inspection_disable_invalid').click())
    await act(async () => button('usage_stats.credentials_inspection_invalid_accounts_confirm').click())
    expect(document.body.textContent).toContain('write failed')
    expect(document.querySelector<HTMLInputElement>('input[type="checkbox"]')?.checked).toBe(true)
    expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(2)
  })

  it.each(['cancel', 'escape'] as const)('restores focus to the trigger button inside the inspection modal when closing via %s', async (closeWay) => {
    await act(async () => root.render(<QuotaInspectionModal open status={status(['a.json', 'b.json'])} loading={false} starting={false} error=""
      onClose={() => undefined} onStart={async () => undefined} onRefreshStatus={async () => undefined} />))
    const parentDialog = document.querySelector<HTMLElement>('[role="dialog"]')!
    const triggerButton = button('usage_stats.credentials_inspection_disable_invalid')
    await act(async () => {
      triggerButton.focus()
      triggerButton.click()
    })
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 0)) })
    const dialogs = document.querySelectorAll<HTMLElement>('[role="dialog"]')
    expect(dialogs).toHaveLength(2)
    expect(dialogs[1].contains(document.activeElement)).toBe(true)

    await act(async () => {
      if (closeWay === 'cancel') {
        button('common.cancel').click()
      } else {
        document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
      }
    })
    expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(1)
    expect(triggerButton.disabled).toBe(false)
    expect(document.activeElement).toBe(triggerButton)
    expect(parentDialog.contains(document.activeElement)).toBe(true)
    expect(document.activeElement).not.toBe(document.body)
  })

  it('keeps focus inside the inspection modal when submitting clears all invalid accounts and disables the trigger', async () => {
    vi.mocked(setCredentialsDisabled).mockResolvedValue({} as Awaited<ReturnType<typeof setCredentialsDisabled>>)
    const normalOnlyStatus: UsageQuotaInspectionStatusResponse = {
      ...status([]),
      total: 1,
      cached: 1,
      normal: 1,
      results: [{ auth_index: 'auth-ok', file_name: 'ok.json', name: 'ok.json', type: 'codex', status: 'normal' }],
    }
    function StatefulInspection() {
      const [currentStatus, setCurrentStatus] = useState<UsageQuotaInspectionStatusResponse>(() => status(['a.json']))
      return (
        <QuotaInspectionModal
          open
          status={currentStatus}
          loading={false}
          starting={false}
          error=""
          onClose={() => undefined}
          onStart={async () => undefined}
          onRefreshStatus={async () => { setCurrentStatus(normalOnlyStatus) }}
        />
      )
    }

    await act(async () => root.render(<StatefulInspection />))
    const parentDialog = document.querySelector<HTMLElement>('[role="dialog"]')!
    const triggerButton = button('usage_stats.credentials_inspection_disable_invalid')
    await act(async () => {
      triggerButton.focus()
      triggerButton.click()
    })
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 0)) })
    const dialogs = document.querySelectorAll<HTMLElement>('[role="dialog"]')
    expect(dialogs).toHaveLength(2)
    expect(dialogs[1].contains(document.activeElement)).toBe(true)

    await act(async () => button('usage_stats.credentials_inspection_invalid_accounts_confirm').click())
    expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(1)
    expect(button('usage_stats.credentials_inspection_disable_invalid').disabled).toBe(true)
    expect(document.activeElement).not.toBe(document.body)
    expect(parentDialog.contains(document.activeElement)).toBe(true)
    expect((document.activeElement as HTMLButtonElement).disabled).not.toBe(true)
  })
})
