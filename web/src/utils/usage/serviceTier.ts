const SERVICE_TIER_LABEL_KEYS = new Map([
  ['auto', 'usage_stats.speed_mode_auto'],
  ['default', 'usage_stats.speed_mode_standard'],
  ['standard', 'usage_stats.speed_mode_standard'],
  ['priority', 'usage_stats.speed_mode_fast'],
  ['fast', 'usage_stats.speed_mode_fast'],
  ['flex', 'usage_stats.speed_mode_flex'],
  ['ultrafast', 'usage_stats.speed_mode_ultrafast'],
]);

type Translate = (key: string) => string;

export const formatServiceTier = (value: unknown, t: Translate): string => {
  const raw = String(value ?? '').trim();
  if (!raw) return '-';
  const labelKey = SERVICE_TIER_LABEL_KEYS.get(raw.toLowerCase());
  return labelKey ? t(labelKey) : raw;
};

// tooltip 与详情共用格式：缺失仅显示横线，非空值保留括号中的原始档位。
export const formatServiceTierDetail = (value: unknown, t: Translate): string => {
  const raw = String(value ?? '').trim();
  if (!raw) return '-';
  return `${formatServiceTier(raw, t)} (${raw})`;
};
