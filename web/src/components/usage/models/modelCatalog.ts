import { providerBrandIconKey, type ProviderBrandIconKey } from '@/components/ProviderBrandIcon'
import type { AvailableModel } from '@/lib/api'

export const MODEL_CATALOG_PREFERENCES_KEY = 'cpa-usage-keeper-models-preferences-v1'
export interface ModelProvider { key: string; label: string; icon?: ProviderBrandIconKey }
export interface CatalogModel extends AvailableModel { provider: ModelProvider }

const aliases: Record<string, string> = { anthropic: 'claude', google: 'gemini', 'google-ai': 'gemini', 'x-ai': 'xai', 'x.ai': 'xai' }
const labels: Readonly<Record<string, string>> = {
  antigravity: 'Antigravity', claude: 'Claude', codex: 'Codex', devin: 'Devin', gemini: 'Gemini', kimi: 'Kimi', meta: 'Meta', openai: 'OpenAI', vertex: 'Vertex', xai: 'xAI',
  deepseek: 'DeepSeek', muse: 'Muse', qwen: 'Qwen', mistral: 'Mistral', minimax: 'MiniMax', zai: 'Z.ai', 'z.ai': 'Z.ai',
}

// 只识别明确的模型家族前缀；边界避免把 gptish、o365 等自定义别名误归类。
const modelPrefixes: ReadonlyArray<readonly [RegExp, string]> = [
  [/^(?:(?:gpt|chatgpt|codex)(?:[-_.:]|\d|$)|o[134](?:[-_.:]|$))/, 'openai'],
  [/^claude(?:[-_.:]|\d|$)/, 'claude'],
  [/^gemini(?:[-_.:]|\d|$)/, 'gemini'],
  [/^grok(?:[-_.:]|\d|$)/, 'xai'],
  [/^(?:kimi|moonshot)(?:[-_.:]|\d|$)/, 'kimi'],
  [/^(?:meta[-_])?llama(?:[-_.:]|\d|$)/, 'meta'],
  [/^deepseek(?:[-_.:]|\d|$)/, 'deepseek'],
  [/^muse(?:[-_.:]|\d|$)/, 'muse'],
  [/^qwen(?:[-_.:]|\d|$)/, 'qwen'],
  [/^(?:mistral|mixtral|codestral|ministral|devstral|pixtral|magistral|voxtral)(?:[-_.:]|\d|$)/, 'mistral'],
  [/^minimax(?:[-_.:]|\d|$)/, 'minimax'],
  [/^(?:chat)?glm(?:[-_.:]|\d|$)/, 'z.ai'],
]

function providerFromOwner(owner?: string): ModelProvider {
  const raw = owner?.trim() || ''
  const normalized = raw.toLowerCase()
  const alias = aliases[normalized]
  const icon = providerBrandIconKey(typeof alias === 'string' ? alias : normalized)
  const label = labels[normalized]
  return typeof icon === 'string' ? { key: `provider:${icon}`, label: labels[icon] ?? raw, icon }
    : { key: raw ? `owner:${normalized}` : 'unknown', label: typeof label === 'string' ? label : raw }
}

export function modelProvider(modelID: string, owner?: string): ModelProvider {
  // xxx/gpt-* 中的 xxx 是路由命名空间；只用最后一段识别，展示和复制仍保留完整 ID。
  const name = modelID.trim().split('/').pop()?.trim().toLowerCase() ?? ''
  const matchedProvider = modelPrefixes.find(([pattern]) => pattern.test(name))?.[1]
  return providerFromOwner(matchedProvider ?? owner)
}

export function normalizeCatalog(models: AvailableModel[]): CatalogModel[] {
  const seen = new Set<string>()
  return models.flatMap((model) => {
    if (!model.id?.trim() || seen.has(model.id)) return []
    seen.add(model.id)
    return [{ ...model, provider: modelProvider(model.id, model.owned_by) }]
  }).sort((a, b) => a.id.localeCompare(b.id))
}

export function modelProviderFromFilter(filter: string): ModelProvider | undefined {
  const match = /^(?:provider|owner):(.+)$/.exec(filter)
  if (!match) return undefined
  const provider = providerFromOwner(match[1])
  return provider.icon ? provider : undefined
}

export function loadModelProviderFilter(): string {
  try {
    const saved = JSON.parse(localStorage.getItem(MODEL_CATALOG_PREFERENCES_KEY) ?? 'null')
    const value = saved?.providerFilter
    if (saved?.version !== 1 || typeof value !== 'string' || value.length > 256 || value === 'all') return 'all'
    // 旧 owner 筛选迁移到有图标的品牌；无图标的旧筛选只计入 All。
    return modelProviderFromFilter(value)?.key ?? 'all'
  } catch { return 'all' }
}

export function persistModelProviderFilter(providerFilter: string): void {
  try { localStorage.setItem(MODEL_CATALOG_PREFERENCES_KEY, JSON.stringify({ version: 1, providerFilter })) }
  catch { /* 存储不可用不应阻止当前页筛选。 */ }
}
