import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { CredentialsPagination, formatCredentialNumber, formatCredentialPercent } from '../CredentialSectionShell'

const renderPagination = (props: Partial<Parameters<typeof CredentialsPagination>[0]> = {}) => renderToStaticMarkup(
  createElement(CredentialsPagination, {
    page: 1, totalPages: 1, pageSize: 10,
    previousLabel: 'Previous', nextLabel: 'Next', rowsPerPageLabel: 'Size',
    onPageChange: () => undefined, onPageSizeChange: () => undefined,
    ...props,
  }),
)

describe('CredentialSectionShell formatting', () => {
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

  it('keeps pagination controls visible for non-empty single-page sections', () => {
    const html = renderPagination({ total: 3 })

    expect(html).toContain('Size')
    expect(html).toContain('1 / 1')
  })

  it('renders an optional sort control before pagination buttons', () => {
    const html = renderPagination({
      leadingControls: createElement('span', null, 'Quota Usage'),
      total: 3,
      sortValue: 'priority',
      sortOptions: [{ value: 'priority', label: 'Priority' }],
      sortLabel: 'Order by',
      rowsPerPageLabel: 'Rows',
      onSortChange: () => undefined,
    })

    expect(html.indexOf('Quota Usage')).toBeLessThan(html.indexOf('Order by'))
    expect(html.indexOf('Order by')).toBeLessThan(html.indexOf('Rows'))
    expect(html).toContain('Priority')
  })
})
