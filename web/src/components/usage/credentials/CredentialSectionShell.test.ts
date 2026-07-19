import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { CredentialSectionShell, CredentialsPagination, formatCredentialNumber, formatCredentialPercent } from './CredentialSectionShell'

describe('CredentialSectionShell formatting', () => {
  it('renders the title area without a label slot', () => {
    const html = renderToStaticMarkup(createElement(CredentialSectionShell, {
      title: 'Auth Files',
      subtitle: 'Credential usage',
      countLabel: '2',
      children: createElement('div', null, 'Rows'),
    }))

    expect(html).toContain('Auth Files')
    expect(html).not.toContain('_credentialSectionEyebrow_')
    expect(html).not.toContain('Credentials')
  })

  it('uses the shared compact K/M/B number format', () => {
    expect(formatCredentialNumber(950)).toBe('950')
    expect(formatCredentialNumber(12_345)).toBe('12.35K')
    expect(formatCredentialNumber(1_234_567)).toBe('1.23M')
  })

  it('formats credential rates with two decimal places', () => {
    expect(formatCredentialPercent(2 / 3 * 100)).toBe('66.67%')
    expect(formatCredentialPercent(75)).toBe('75.00%')
    expect(formatCredentialPercent(null)).toBe('—')
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
    expect(html).toContain('ant-pagination')
    expect(html).toContain('10 Size')
    expect(html).toContain('aria-label="Page Size"')
    expect(html).not.toContain('Rows per page')
    expect(html).not.toContain('<select')
  })

  it('keeps pagination controls visible for non-empty single-page sections', () => {
    const html = renderToStaticMarkup(createElement(CredentialsPagination, {
      page: 1,
      total: 3,
      totalPages: 1,
      pageSize: 10,
      previousLabel: 'Previous',
      nextLabel: 'Next',
      rowsPerPageLabel: 'Size',
      onPageChange: () => undefined,
      onPageSizeChange: () => undefined,
    }))

    expect(html).toContain('Size')
    expect(html).toContain('1 / 1')
  })

  it('renders an optional sort control before pagination buttons', () => {
    const html = renderToStaticMarkup(createElement(CredentialsPagination, {
      leadingControls: createElement('span', null, 'Quota Usage'),
      page: 1,
      total: 3,
      totalPages: 1,
      pageSize: 10,
      sortValue: 'priority',
      sortOptions: [{ value: 'priority', label: 'Priority' }],
      sortLabel: 'Order by',
      previousLabel: 'Previous',
      nextLabel: 'Next',
      rowsPerPageLabel: 'Rows',
      onPageChange: () => undefined,
      onPageSizeChange: () => undefined,
      onSortChange: () => undefined,
    }))

    expect(html.indexOf('Quota Usage')).toBeLessThan(html.indexOf('Order by'))
    expect(html.indexOf('Order by')).toBeLessThan(html.indexOf('Rows'))
    expect(html).toContain('Priority')
  })
})
