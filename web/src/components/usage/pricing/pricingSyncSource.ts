import type { PricingSyncSource } from '@/lib/types'

const sourceStorageKey = 'cpa-pricing-sync-source-v1'

// 同步弹窗只持久化来源选择；预览和提交草稿始终属于当前弹窗。
export function readPricingSyncSource(): PricingSyncSource {
  try {
    return window.localStorage.getItem(sourceStorageKey) === 'litellm' ? 'litellm' : 'models-dev'
  } catch {
    return 'models-dev'
  }
}

export function storePricingSyncSource(source: PricingSyncSource): void {
  try { window.localStorage.setItem(sourceStorageKey, source) } catch { /* 浏览器存储不可用时仅保留当前选择。 */ }
}
