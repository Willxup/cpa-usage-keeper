// @vitest-environment happy-dom
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { describe, expect, it } from 'vitest'
import { AiProviderCredentialsSection } from '../AiProviderCredentialsSection'
import { CredentialsSection } from '../CredentialsSection'
import { createAiProviderSectionProps, createCredentialSectionProps } from './credentialSectionFixtures'

globalThis.IS_REACT_ACT_ENVIRONMENT = true

describe.each(['cards', 'list'] as const)('credential section headers in %s mode', (viewMode) => {
  it.each(['credentials', 'providers'] as const)('does not render a subtitle or placeholder for %s', async (section) => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    try {
      await act(async () => root.render(section === 'credentials'
        ? <CredentialsSection {...createCredentialSectionProps({ viewMode })} />
        : <AiProviderCredentialsSection {...createAiProviderSectionProps({ viewMode })} />))
      const titleRow = container.querySelector('.keeper-card-title-track')!

      expect(titleRow.querySelector('h3')).not.toBeNull()
      expect(container.querySelector('.keeper-card-subtitle')).toBeNull()
      expect(titleRow.parentElement?.children).toHaveLength(1)
    } finally {
      await act(async () => root.unmount())
      container.remove()
    }
  })
})
