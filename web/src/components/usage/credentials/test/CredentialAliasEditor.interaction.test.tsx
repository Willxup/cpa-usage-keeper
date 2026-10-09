// @vitest-environment happy-dom
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { expect, it, vi } from 'vitest'
import { CredentialAliasEditor } from '../CredentialAliasEditor'

globalThis.IS_REACT_ACT_ENVIRONMENT = true
vi.mock('react-i18next', () => ({ useTranslation: () => ({ t: (key: string) => key }) }))

it('opens the unified editor from the pencil while the name still opens details', async () => {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  const onEdit = vi.fn()
  const onOpenDetails = vi.fn()
  try {
    await act(async () => root.render(<CredentialAliasEditor identityId="1" displayName="Account" onEdit={onEdit} onOpenDetails={onOpenDetails} />))
    await act(async () => container.querySelector<HTMLButtonElement>('[aria-label="usage_stats.credentials_edit_title"]')!.click())
    expect(onEdit).toHaveBeenCalledTimes(1)
    expect(onOpenDetails).not.toHaveBeenCalled()
    expect(container.querySelector('input')).toBeNull()
    await act(async () => container.querySelector<HTMLButtonElement>('[data-credential-detail-trigger]')!.click())
    expect(onOpenDetails).toHaveBeenCalledTimes(1)
  } finally {
    await act(async () => root.unmount())
    container.remove()
  }
})
