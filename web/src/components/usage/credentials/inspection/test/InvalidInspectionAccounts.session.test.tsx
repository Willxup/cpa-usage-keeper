// @vitest-environment happy-dom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { setCredentialsDisabled } from '@/lib/api'
import type { UsageQuotaInspectionStatusResponse } from '@/lib/types'
import { QuotaInspectionModal } from '../QuotaInspectionModal'

vi.mock('@/lib/api', async (original) => ({
  ...await original<typeof import('@/lib/api')>(),
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
})
