import { describe, expect, it } from 'vitest';
import {
  INPUT_TOKEN_DISPLAY_MODE_STORAGE_KEY,
  loadInputTokenDisplayMode,
  saveInputTokenDisplayMode,
} from '../inputTokenDisplayPreference';

function createStorage(initial?: string) {
  let value = initial ?? null;

  return {
    getItem: () => value,
    setItem: (_key: string, next: string) => {
      value = next;
    },
    value: () => value,
  };
}

describe('input token display preference', () => {
  it('defaults to split mode', () => {
    expect(loadInputTokenDisplayMode(undefined)).toBe('split');
  });

  it('loads total mode from storage', () => {
    const storage = createStorage('total');

    expect(loadInputTokenDisplayMode(storage)).toBe('total');
  });

  it('falls back to split mode for invalid stored values', () => {
    const storage = createStorage('invalid');

    expect(loadInputTokenDisplayMode(storage)).toBe('split');
  });

  it('persists the selected mode', () => {
    const storage = createStorage();

    saveInputTokenDisplayMode('total', storage);

    expect(storage.value()).toBe('total');
    expect(INPUT_TOKEN_DISPLAY_MODE_STORAGE_KEY).toBe(
      'cli-proxy-usage-input-token-display-mode-v1',
    );
  });
});