import type { UsageIdentity } from '@/lib/types'

export type CredentialProviderFilterKey = 'all' | 'antigravity' | 'claude' | 'codex' | 'gemini-cli' | 'iflow'

export interface CredentialProviderFilterOption {
  key: CredentialProviderFilterKey
  labelKey: string
}

export interface CredentialProviderRowLike {
  identity: Pick<UsageIdentity, 'name' | 'displayName' | 'type' | 'provider'>
}

export const CREDENTIAL_PROVIDER_FILTER_OPTIONS: CredentialProviderFilterOption[] = [
  { key: 'all', labelKey: 'usage_stats.credentials_filter_all' },
  { key: 'antigravity', labelKey: 'usage_stats.credentials_filter_antigravity' },
  { key: 'claude', labelKey: 'usage_stats.credentials_filter_claude' },
  { key: 'codex', labelKey: 'usage_stats.credentials_filter_codex' },
  { key: 'gemini-cli', labelKey: 'usage_stats.credentials_filter_gemini_cli' },
  { key: 'iflow', labelKey: 'usage_stats.credentials_filter_iflow' },
]

const PROVIDER_ALIASES: Record<Exclude<CredentialProviderFilterKey, 'all'>, string[]> = {
  antigravity: ['antigravity'],
  claude: ['claude', 'anthropic', 'claudecode'],
  codex: ['codex'],
  'gemini-cli': ['gemini', 'geminicli'],
  iflow: ['iflow', 'flowith'],
}

export function normalizeCredentialProviderToken(value: string | undefined): string {
  return (value ?? '').toLowerCase().replace(/[^a-z0-9]/g, '')
}

const NORMALIZED_PROVIDER_ALIASES = (Object.entries(PROVIDER_ALIASES) as Array<[Exclude<CredentialProviderFilterKey, 'all'>, string[]]>).map(
  ([key, aliases]) => ({
    key,
    normalizedAliases: aliases.map(normalizeCredentialProviderToken),
  }),
)

export function resolveCredentialProviderKey(row: CredentialProviderRowLike): CredentialProviderFilterKey | null {
  const tokens = [
    row.identity.type,
    row.identity.provider,
    row.identity.name,
    row.identity.displayName,
  ].map(normalizeCredentialProviderToken).filter(Boolean)

  for (const { key, normalizedAliases } of NORMALIZED_PROVIDER_ALIASES) {
    if (tokens.some((token) => normalizedAliases.some((alias) => token === alias || token.includes(alias)))) {
      return key
    }
  }

  return null
}

export function matchesCredentialProviderFilter(row: CredentialProviderRowLike, filter: CredentialProviderFilterKey): boolean {
  return filter === 'all' || resolveCredentialProviderKey(row) === filter
}

export function filterCredentialsByProvider<T extends CredentialProviderRowLike>(rows: T[], filter: CredentialProviderFilterKey): T[] {
  if (filter === 'all') {
    return rows
  }
  return rows.filter((row) => matchesCredentialProviderFilter(row, filter))
}

export function buildCredentialProviderFilterCounts(rows: CredentialProviderRowLike[]): Record<CredentialProviderFilterKey, number> {
  const counts = Object.fromEntries(CREDENTIAL_PROVIDER_FILTER_OPTIONS.map((option) => [option.key, 0])) as Record<CredentialProviderFilterKey, number>
  counts.all = rows.length

  for (const row of rows) {
    const key = resolveCredentialProviderKey(row)
    if (key) {
      counts[key] += 1
    }
  }

  return counts
}
