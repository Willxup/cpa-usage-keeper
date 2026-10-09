const normalizeModelName = (value: unknown): string => String(value ?? '').trim()

const comparableModelName = (value: string): string => value.toLowerCase()

export interface UsageModelDisplay {
  model: string
  responseModel: string
  modelAlias: string
}

export interface UsageModelTooltip {
  model: string
  responseModel: string
  modelAlias: string
}

export interface UsageModelTooltipLabels {
  model: string
  responseModel: string
  modelAlias: string
}

export const getUsageModelDisplay = (
  modelValue: unknown,
  responseModelValue: unknown,
  modelAliasValue: unknown,
): UsageModelDisplay => {
  const model = normalizeModelName(modelValue)
  const responseModel = normalizeModelName(responseModelValue)
  const modelAlias = normalizeModelName(modelAliasValue)
  const comparableModel = comparableModelName(model)

  return {
    model: model || '-',
    responseModel: responseModel && comparableModelName(responseModel) !== comparableModel
      ? responseModel
      : '',
    modelAlias: modelAlias && comparableModelName(modelAlias) !== comparableModel
      ? modelAlias
      : '',
  }
}

// Tooltip 保留原始的非空模型值，不复用列表展示的“相同则隐藏”规则。
export const getUsageModelTooltip = (
  modelValue: unknown,
  responseModelValue: unknown,
  modelAliasValue: unknown,
): UsageModelTooltip => ({
  model: normalizeModelName(modelValue),
  responseModel: normalizeModelName(responseModelValue),
  modelAlias: normalizeModelName(modelAliasValue),
})

export const buildUsageModelTooltipLines = (
  values: UsageModelTooltip,
  labels: UsageModelTooltipLabels,
  formatLine: (label: string, value: string) => string,
): string[] => [
  [labels.model, values.model],
  [labels.responseModel, values.responseModel],
  [labels.modelAlias, values.modelAlias],
].filter(([, value]) => value.length > 0)
  .map(([label, value]) => formatLine(label, value))
