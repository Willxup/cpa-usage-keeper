import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { CredentialsPagination, formatCredentialNumber } from './CredentialSectionShell'

describe('CredentialSectionShell formatting', () => {
  it('uses the shared compact K/M/B number format', () => {
    expect(formatCredentialNumber(950)).toBe('950')
    expect(formatCredentialNumber(12_345)).toBe('12.35K')
    expect(formatCredentialNumber(1_234_567)).toBe('1.23M')
  })

  it('renders only controls in the pagination footer', () => {
    const html = renderToStaticMarkup(createElement(CredentialsPagination, {
      page: 2,
      totalPages: 5,
      pageSize: 10,
      previousLabel: 'Previous',
      nextLabel: 'Next',
      rowsPerPageLabel: 'Size',
      onPageChange: () => undefined,
      onPageSizeChange: () => undefined,
    }))

    expect(html).not.toContain('_credentialPaginationRange_')
    expect(html).not.toContain('11–20 / 42')
    expect(html).toContain('Size')
    expect(html).not.toContain('Rows per page')
    expect(html).toContain('<select')
    expect(html).toContain('value="10"')
  })
})
