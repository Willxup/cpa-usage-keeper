import type { InputTokenDisplayMode } from '@/utils/usage';

export const INPUT_TOKEN_DISPLAY_MODE_STORAGE_KEY =
  'cli-proxy-usage-input-token-display-mode-v1';

export function loadInputTokenDisplayMode(
  storage: Pick<Storage, 'getItem'> | undefined = typeof localStorage === 'undefined'
    ? undefined
    : localStorage,
): InputTokenDisplayMode {
  const value = storage?.getItem(INPUT_TOKEN_DISPLAY_MODE_STORAGE_KEY);
  return value === 'total' ? 'total' : 'split';
}

export function saveInputTokenDisplayMode(
  mode: InputTokenDisplayMode,
  storage: Pick<Storage, 'setItem'> | undefined = typeof localStorage === 'undefined'
    ? undefined
    : localStorage,
): void {
  storage?.setItem(INPUT_TOKEN_DISPLAY_MODE_STORAGE_KEY, mode);
}