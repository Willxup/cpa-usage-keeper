import { providerBrandIconKey, type ProviderBrandIconKey } from '@/components/ProviderBrandIcon'
import type { AvailableModel } from '@/lib/api'

export const MODEL_CATALOG_PREFERENCES_KEY = 'cpa-usage-keeper-models-preferences-v1'
export interface ModelProvider { key: string; label: string; icon?: ProviderBrandIconKey }
export interface CatalogModel extends AvailableModel { provider: ModelProvider }

const aliases: Record<string, string> = { anthropic: 'claude', google: 'gemini', 'google-ai': 'gemini', 'x-ai': 'xai', 'x.ai': 'xai' }
const labels: Partial<Record<ProviderBrandIconKey, string>> = {
  antigravity: 'Antigravity', claude: 'Claude', codex: 'Codex', devin: 'Devin', gemini: 'Gemini', kimi: 'Kimi', meta: 'Meta', openai: 'OpenAI', vertex: 'Vertex', xai: 'xAI',
}

// 只根据 CPA 的 owned_by 归类，不从模型名称猜测供应商（别名可能与真实提供方不同）。
export function modelProvider(owner?: string): ModelProvider {
  const raw = owner?.trim() || ''
  const normalized = raw.toLowerCase()
  const icon = providerBrandIconKey(aliases[normalized] ?? normalized)
  return icon ? { key: `provider:${icon}`, label: labels[icon] ?? raw, icon }
    : { key: raw ? `owner:${normalized}` : 'unknown', label: raw }
}

export function normalizeCatalog(models: AvailableModel[]): CatalogModel[] {
  const seen = new Set<string>()
  return models.flatMap((model) => {
    if (!model.id?.trim() || seen.has(model.id)) return []
    seen.add(model.id)
    return [{ ...model, provider: modelProvider(model.owned_by) }]
  }).sort((a, b) => a.id.localeCompare(b.id))
}

export function loadModelProviderFilter(): string {
  try {
    const saved = JSON.parse(localStorage.getItem(MODEL_CATALOG_PREFERENCES_KEY) ?? 'null')
    const value = saved?.providerFilter
    return saved?.version === 1 && typeof value === 'string' && value.length <= 256 &&
      (value === 'all' || value === 'unknown' || /^(provider|owner):\S/.test(value)) ? value : 'all'
  } catch { return 'all' }
}

export function persistModelProviderFilter(providerFilter: string): void {
  try { localStorage.setItem(MODEL_CATALOG_PREFERENCES_KEY, JSON.stringify({ version: 1, providerFilter })) }
  catch { /* 存储不可用不应阻止当前页筛选。 */ }
}
