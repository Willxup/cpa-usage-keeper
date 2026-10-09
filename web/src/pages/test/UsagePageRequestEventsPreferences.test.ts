import { describe, expect, it, vi } from 'vitest';
import { REQUEST_EVENT_COLUMN_IDS } from '@/components/usage/RequestEventsDetailsCard';
import { normalizeRequestEventsPreferences, loadRequestEventsPreferences, saveRequestEventsPreferences, REQUEST_EVENTS_PREFERENCES_STORAGE_KEY } from '../UsagePage';

const createMemoryStorage = (seed: Record<string, string> = {}) => {
  const values = new Map(Object.entries(seed));
  return {
    getItem: vi.fn((key: string) => values.get(key) ?? null),
    setItem: vi.fn((key: string, value: string) => {
      values.set(key, value);
    }),
    value: (key: string) => values.get(key),
  };
};

describe('UsagePage request event column preferences', () => {
  it('resets column visibility and order from every legacy preference version', () => {
    for (const version of [1, 2, 3, 4, 5, 6, 7, 8]) {
      const preferences = normalizeRequestEventsPreferences({
        version,
        filters: {
          model: 'gpt-5.6',
          source: 'openai-team',
          result: 'failed',
        },
        visibleColumnIds: ['timestamp', 'model_alias', 'total_cost'],
        columnOrder: ['total_cost', 'model_alias', 'timestamp'],
      });

      expect(preferences).toEqual({
        version: 9,
        filters: {
          model: 'gpt-5.6',
          source: 'openai-team',
          result: 'failed',
        },
        visibleColumnIds: REQUEST_EVENT_COLUMN_IDS,
        columnOrder: REQUEST_EVENT_COLUMN_IDS,
      });
    }
  });

  it('resets column settings when the saved preference has no recognized version', () => {
    const preferences = normalizeRequestEventsPreferences({
      filters: {
        model: 'claude-sonnet',
        source: 'anthropic-team',
        result: 'success',
      },
      visibleColumnIds: ['timestamp', 'model'],
      columnOrder: ['model', 'timestamp'],
    });

    expect(preferences.filters).toEqual({
      model: 'claude-sonnet',
      source: 'anthropic-team',
      result: 'success',
    });
    expect(preferences.visibleColumnIds).toEqual(REQUEST_EVENT_COLUMN_IDS);
    expect(preferences.columnOrder).toEqual(REQUEST_EVENT_COLUMN_IDS);
  });

  it('preserves and normalizes custom column settings from the current version', () => {
    const preferences = normalizeRequestEventsPreferences({
      version: 9,
      filters: { model: 'gpt-5', apiKeyId: '22', source: 'team', result: 'failed' },
      visibleColumnIds: ['model', 'timestamp', 'model', 'not-a-column', 'total_cost'],
      columnOrder: ['total_cost', 'timestamp', 'total_cost', 'not-a-column'],
    });

    expect(preferences.filters).toEqual({ model: 'gpt-5', source: 'team', result: 'failed' });
    expect(preferences.visibleColumnIds).toEqual(['model', 'timestamp', 'total_cost']);
    expect(preferences.columnOrder).toEqual([
      'total_cost',
      'timestamp',
      ...REQUEST_EVENT_COLUMN_IDS.filter((columnId) => columnId !== 'total_cost' && columnId !== 'timestamp'),
    ]);
  });

  it('falls back to all compact columns for damaged current-version settings', () => {
    const preferences = normalizeRequestEventsPreferences({
      version: 9,
      visibleColumnIds: ['not-a-column'],
      columnOrder: 'not-an-array',
    });

    expect(preferences.visibleColumnIds).toEqual(REQUEST_EVENT_COLUMN_IDS);
    expect(preferences.columnOrder).toEqual(REQUEST_EVENT_COLUMN_IDS);
  });
});

describe('UsagePage request event preferences', () => {
  it('falls back safely for damaged persisted request event preferences', () => {
    const preferences = normalizeRequestEventsPreferences({
      version: 9,
      filters: {
        model: 42,
        source: '',
        result: 'maybe',
      },
      visibleColumnIds: ['not-a-column'],
    });

    expect(preferences.filters).toEqual({
      model: '__all__',
      source: '__all__',
      result: '__all__',
    });
    expect(preferences.visibleColumnIds[0]).toBe('timestamp');
    expect(preferences.visibleColumnIds.length).toBeGreaterThan(1);
  });

  it.each(['speed', 'service_tier'])('preserves preferences hiding %s', (hiddenColumn) => {
    const storage = createMemoryStorage();
    const visibleColumnIds = REQUEST_EVENT_COLUMN_IDS.filter((columnId) => columnId !== hiddenColumn);
    const preferences = {
      version: 9,
      filters: { model: '__all__', source: '__all__', result: '__all__' },
      visibleColumnIds,
      columnOrder: [...REQUEST_EVENT_COLUMN_IDS],
    };

    saveRequestEventsPreferences(preferences, storage);
    expect(JSON.parse(storage.value(REQUEST_EVENTS_PREFERENCES_STORAGE_KEY)!)).toEqual(preferences);
    expect(loadRequestEventsPreferences(storage).visibleColumnIds).toEqual(visibleColumnIds);
  });

  it('loads defaults from invalid JSON and persists normalized request event preferences', () => {
    const storage = createMemoryStorage({
      [REQUEST_EVENTS_PREFERENCES_STORAGE_KEY]: '{bad json',
    });

    expect(loadRequestEventsPreferences(storage).filters).toEqual({
      model: '__all__',
      source: '__all__',
      result: '__all__',
    });

    saveRequestEventsPreferences({
      version: 9,
      filters: {
        model: 'gpt-4.1',
        source: 'source-a',
        result: 'success',
      },
      visibleColumnIds: ['timestamp', 'timestamp', 'model'],
    }, storage);

    expect(storage.setItem).toHaveBeenCalledTimes(1);
    expect(JSON.parse(storage.value(REQUEST_EVENTS_PREFERENCES_STORAGE_KEY) ?? '')).toEqual({
      version: 9,
      filters: {
        model: 'gpt-4.1',
        source: 'source-a',
        result: 'success',
      },
      visibleColumnIds: ['timestamp', 'model'],
      columnOrder: REQUEST_EVENT_COLUMN_IDS,
    });
  });
});
