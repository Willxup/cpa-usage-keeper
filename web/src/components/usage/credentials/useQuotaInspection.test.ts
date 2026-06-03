import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
import { shouldContinueQuotaInspectionPolling, shouldNotifyQuotaInspectionCompleted } from './useQuotaInspection'

const quotaInspectionSource = readFileSync(new URL('./useQuotaInspection.ts', import.meta.url), 'utf8').replace(/\r\n/g, '\n')

describe('useQuotaInspection polling', () => {
  it('polls only while an inspection round is actively running', () => {
    expect(shouldContinueQuotaInspectionPolling({ running: true, completed: false })).toBe(true)
    expect(shouldContinueQuotaInspectionPolling({ running: true, completed: true })).toBe(false)
    expect(shouldContinueQuotaInspectionPolling({ running: false, completed: false })).toBe(false)
    expect(shouldContinueQuotaInspectionPolling(null)).toBe(false)
  })

  it('notifies listeners only when an inspection round has completed', () => {
    expect(shouldNotifyQuotaInspectionCompleted({ running: false, completed: true })).toBe(true)
    expect(shouldNotifyQuotaInspectionCompleted({ running: true, completed: false })).toBe(false)
    expect(shouldNotifyQuotaInspectionCompleted({ running: false, completed: false })).toBe(false)
    expect(shouldNotifyQuotaInspectionCompleted(null)).toBe(false)
  })

  it('resumes polling from the initial enabled status load when a round is running', () => {
    const start = quotaInspectionSource.indexOf('const loadInitialInspectionStatus = async () => {')
    const end = quotaInspectionSource.indexOf('void loadInitialInspectionStatus()')

    expect(start).toBeGreaterThanOrEqual(0)
    expect(end).toBeGreaterThan(start)

    const initialLoadBlock = quotaInspectionSource.slice(start, end)
    expect(initialLoadBlock).toContain('const response = await loadQuotaInspectionStatus(controller.signal)')
    expect(initialLoadBlock).toContain('setInspectionPollingActive(shouldContinueQuotaInspectionPolling(response))')
    expect(initialLoadBlock).not.toContain('setTimeout')
    expect(initialLoadBlock).not.toContain('pollQuotaInspectionStatus')
  })

  it('calls the completion callback when active inspection polling finishes', () => {
    expect(quotaInspectionSource).toContain('onInspectionCompleted?: () => void')
    expect(quotaInspectionSource).toContain('if (shouldNotifyQuotaInspectionCompleted(response))')
    expect(quotaInspectionSource).toContain('onInspectionCompletedRef.current?.()')
  })

  it('keeps external callbacks in refs so polling does not restart on parent rerenders', () => {
    expect(quotaInspectionSource).toContain('useRef')
    expect(quotaInspectionSource).toContain('const onAuthRequiredRef = useRef(onAuthRequired)')
    expect(quotaInspectionSource).toContain('const onInspectionCompletedRef = useRef(onInspectionCompleted)')
    expect(quotaInspectionSource).toContain('onAuthRequiredRef.current?.()')
    expect(quotaInspectionSource).not.toContain('[enabled, inspectionPollingActive, loadQuotaInspectionStatus, onInspectionCompleted]')
  })
})
