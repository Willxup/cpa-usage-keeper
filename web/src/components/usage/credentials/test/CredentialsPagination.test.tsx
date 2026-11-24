// @vitest-environment happy-dom

import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { CredentialsPagination } from '../CredentialSectionShell'

globalThis.IS_REACT_ACT_ENVIRONMENT = true

describe('CredentialsPagination', () => {
  let container: HTMLDivElement
  let root: ReturnType<typeof createRoot>

  beforeEach(() => {
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })

  afterEach(() => {
    act(() => root.unmount())
    container.remove()
  })

  it('uses the shared custom selects for sorting and page size', () => {
    const onSortChange = vi.fn()
    const onPageSizeChange = vi.fn()

    act(() => root.render(
      <CredentialsPagination
        page={1}
        total={30}
        totalPages={3}
        pageSize={10}
        sortValue="priority"
        sortOptions={[
          { value: 'priority', label: 'Priority' },
          { value: 'total_requests', label: 'Total Requests' },
        ]}
        sortLabel="Order by"
        previousLabel="Previous"
        nextLabel="Next"
        rowsPerPageLabel="Rows per page"
        onPageChange={() => undefined}
        onPageSizeChange={onPageSizeChange}
        onSortChange={onSortChange}
      />,
    ))

    expect(container.querySelector('select')).toBeNull()
    const sortSizer = container.querySelector<HTMLElement>('[data-credential-pagination-sort-sizer="true"]')
    expect(sortSizer?.getAttribute('aria-hidden')).toBe('true')
    expect(sortSizer?.textContent).toContain('Priority')
    expect(sortSizer?.textContent).toContain('Total Requests')

    const sortTrigger = container.querySelector<HTMLButtonElement>('button[aria-label="Order by: Priority"]')
    expect(sortTrigger!.textContent).toContain('Priority')
    act(() => sortTrigger!.click())
    const totalRequestsOption = Array.from(document.querySelectorAll<HTMLButtonElement>('[role="option"]'))
      .find((option) => option.textContent?.includes('Total Requests'))
    act(() => totalRequestsOption!.click())
    expect(onSortChange).toHaveBeenCalledWith('total_requests')

    const pageSizeTrigger = container.querySelector<HTMLButtonElement>('button[aria-label="Rows per page: 10"]')
    expect(pageSizeTrigger!.textContent).toContain('10')
    act(() => pageSizeTrigger!.click())
    const pageSizeOption = Array.from(document.querySelectorAll<HTMLButtonElement>('[role="option"]'))
      .find((option) => option.textContent?.trim() === '20')
    act(() => pageSizeOption!.click())
    expect(onPageSizeChange).toHaveBeenCalledWith(20)
  })

  it('scrolls overflowing pagination focus without scrolling a Select portal option', () => {
    act(() => root.render(
      <CredentialsPagination
        page={1} total={30} totalPages={3} pageSize={10}
        sortValue="priority" sortOptions={[{ value: 'priority', label: 'Priority' }]}
        sortLabel="Sort" previousLabel="Previous" nextLabel="Next" rowsPerPageLabel="Size"
        onPageChange={() => undefined} onPageSizeChange={() => undefined} onSortChange={() => undefined}
      />,
    ))
    const pagination = container.firstElementChild!
    Object.defineProperties(pagination, { clientWidth: { value: 100 }, scrollWidth: { value: 200 } })
    const next = Array.from(container.querySelectorAll<HTMLButtonElement>('button')).find((button) => button.textContent === 'Next')!
    const scrollNext = vi.fn()
    next.scrollIntoView = scrollNext
    act(() => next.focus())
    expect(scrollNext).toHaveBeenCalledWith({ block: 'nearest', inline: 'nearest' })

    act(() => container.querySelector<HTMLButtonElement>('button[aria-label="Sort: Priority"]')!.click())
    const option = document.querySelector<HTMLButtonElement>('[role="option"]')!
    expect(container.contains(option)).toBe(false)
    const scrollOption = vi.fn()
    option.scrollIntoView = scrollOption
    act(() => option.focus())
    expect(document.activeElement).toBe(option)
    expect(scrollOption).not.toHaveBeenCalled()
  })
})
