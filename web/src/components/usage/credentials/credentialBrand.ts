// CPA 类型保持原样；此映射仅用于认证文件界面的品牌展示。
const META_CREDENTIAL_BRAND = {
  iconType: 'muse',
  name: 'Muse',
  filterLabelKey: 'usage_stats.credentials_filter_muse',
} as const

export function credentialBrandOverride(type: string | null | undefined) {
  return type?.trim().toLowerCase() === 'meta' ? META_CREDENTIAL_BRAND : undefined
}
