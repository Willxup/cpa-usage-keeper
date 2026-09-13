export const DEFAULT_ANALYSIS_COMPOSITION_ITEM_LIMIT = 5;
export const ANALYSIS_COMPOSITION_ITEM_LIMIT_STORAGE_KEY = 'cpa.analysis.usageDistributionLimit';

type ReadableStorage = Pick<Storage, 'getItem'>;
type WritableStorage = Pick<Storage, 'setItem'>;

const getBrowserStorage = (): Storage | undefined => {
  if (typeof window === 'undefined') return undefined;
  try {
    return window.localStorage;
  } catch {
    return undefined;
  }
};

export function parseAnalysisCompositionItemLimit(value: unknown): number | null {
  const normalized = typeof value === 'number'
    ? String(value)
    : typeof value === 'string'
      ? value.trim()
      : '';
  if (!/^\d+$/.test(normalized)) return null;

  const parsed = Number(normalized);
  return Number.isSafeInteger(parsed) && parsed >= 0 ? parsed : null;
}

export function loadAnalysisCompositionItemLimit(storage?: ReadableStorage): number {
  const resolvedStorage = storage ?? getBrowserStorage();
  if (!resolvedStorage) return DEFAULT_ANALYSIS_COMPOSITION_ITEM_LIMIT;
  try {
    return parseAnalysisCompositionItemLimit(
      resolvedStorage.getItem(ANALYSIS_COMPOSITION_ITEM_LIMIT_STORAGE_KEY),
    ) ?? DEFAULT_ANALYSIS_COMPOSITION_ITEM_LIMIT;
  } catch {
    return DEFAULT_ANALYSIS_COMPOSITION_ITEM_LIMIT;
  }
}

export function persistAnalysisCompositionItemLimit(value: number, storage?: WritableStorage): void {
  if (parseAnalysisCompositionItemLimit(value) === null) return;

  const resolvedStorage = storage ?? getBrowserStorage();
  if (!resolvedStorage) return;
  try {
    resolvedStorage.setItem(ANALYSIS_COMPOSITION_ITEM_LIMIT_STORAGE_KEY, String(value));
  } catch {
    // Storage may be unavailable in private browsing; the current in-memory setting still applies.
  }
}
