import { describe, expect, it } from 'vitest';
import { REQUEST_EVENT_COLUMN_IDS } from '@/components/usage/RequestEventsDetailsCard';
import { normalizeRequestEventsPreferences } from '../UsagePage';

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
      visibleColumnIds: ['model', 'timestamp', 'model', 'not-a-column', 'total_cost'],
      columnOrder: ['total_cost', 'timestamp', 'total_cost', 'not-a-column'],
    });

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
