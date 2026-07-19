import { describe, expect, it } from 'vitest';
import {
  normalizeRequestEventsPreferences,
} from '../UsagePage';
import { REQUEST_EVENT_COLUMN_IDS } from '@/components/usage/RequestEventsDetailsCard';

const LEGACY_V3_FULL_COLUMNS = [
  'timestamp',
  'api_key',
  'source',
  'model',
  'model_alias',
  'reasoning_effort',
  'service_tier',
  'result',
  'request_type',
  'endpoint',
  'ttft',
  'latency',
  'speed',
  'input_tokens',
  'output_tokens',
  'reasoning_tokens',
  'cached_tokens',
  'cache_rate',
  'total_tokens',
  'total_cost',
];

const LEGACY_V2_FULL_COLUMNS = LEGACY_V3_FULL_COLUMNS.filter((columnId) => columnId !== 'model_alias');
const LEGACY_V1_FULL_COLUMNS = LEGACY_V2_FULL_COLUMNS.filter((columnId) => columnId !== 'service_tier');
const LEGACY_V4_FULL_COLUMNS = REQUEST_EVENT_COLUMN_IDS.map((columnId) => (
  columnId === 'cache_read_rate' ? 'cache_rate' : columnId
));
const MERGED_V6_FULL_COLUMNS = REQUEST_EVENT_COLUMN_IDS.flatMap((columnId) => (
  columnId === 'service_tier' ? [columnId, 'response_service_tier'] : [columnId]
));

describe('UsagePage request event cache column preferences', () => {
  it('upgrades a v3 full selection to all v5 columns including cache write', () => {
    const preferences = normalizeRequestEventsPreferences({
      version: 3,
      pageSize: 100,
      visibleColumnIds: LEGACY_V3_FULL_COLUMNS,
    });

    expect(preferences.version).toBe(5);
    expect(preferences.visibleColumnIds).toEqual(REQUEST_EVENT_COLUMN_IDS);
    expect(preferences.visibleColumnIds).toContain('cache_read_tokens');
    expect(preferences.visibleColumnIds).toContain('cache_creation_tokens');
  });

  it('normalizes a merged v6 full selection back to the combined v5 column', () => {
    const preferences = normalizeRequestEventsPreferences({
      version: 6,
      pageSize: 100,
      visibleColumnIds: MERGED_V6_FULL_COLUMNS,
    });

    expect(preferences.version).toBe(5);
    expect(preferences.visibleColumnIds).toEqual(REQUEST_EVENT_COLUMN_IDS);
    expect(preferences.visibleColumnIds).not.toContain('response_service_tier' as never);
  });

  it('maps a merged v6 response-only selection to the combined Speed Mode column', () => {
    const preferences = normalizeRequestEventsPreferences({
      version: 6,
      pageSize: 100,
      visibleColumnIds: ['timestamp', 'response_service_tier', 'total_tokens'],
    });

    expect(preferences.visibleColumnIds).toEqual(['timestamp', 'service_tier', 'total_tokens']);
  });

  it('upgrades a v4 full selection and maps cache rate to cache read rate', () => {
    const preferences = normalizeRequestEventsPreferences({
      version: 4,
      pageSize: 100,
      visibleColumnIds: LEGACY_V4_FULL_COLUMNS,
    });

    expect(preferences.visibleColumnIds).toEqual(REQUEST_EVENT_COLUMN_IDS);
    expect(preferences.visibleColumnIds).toContain('cache_read_rate');
    expect(preferences.visibleColumnIds).not.toContain('cache_rate' as never);
  });

  it('maps a v3 custom cached column to cache read without adding cache write', () => {
    const preferences = normalizeRequestEventsPreferences({
      version: 3,
      pageSize: 100,
      visibleColumnIds: ['timestamp', 'cached_tokens', 'total_tokens'],
    });

    expect(preferences.visibleColumnIds).toEqual(['timestamp', 'cache_read_tokens', 'total_tokens']);
    expect(preferences.visibleColumnIds).not.toContain('cache_creation_tokens');
  });

  it('upgrades v2 and v1 full selections to all v4 columns', () => {
    for (const [version, visibleColumnIds] of [
      [2, LEGACY_V2_FULL_COLUMNS],
      [1, LEGACY_V1_FULL_COLUMNS],
    ] as const) {
      const preferences = normalizeRequestEventsPreferences({ version, pageSize: 100, visibleColumnIds });
      expect(preferences.visibleColumnIds).toEqual(REQUEST_EVENT_COLUMN_IDS);
    }
  });

  it('keeps legacy custom selections custom while mapping cached to cache read', () => {
    const preferences = normalizeRequestEventsPreferences({
      version: 2,
      pageSize: 100,
      visibleColumnIds: ['timestamp', 'cached_tokens', 'speed'],
    });

    expect(preferences.visibleColumnIds).toEqual(['timestamp', 'cache_read_tokens', 'speed']);
    expect(preferences.visibleColumnIds).not.toContain('model_alias');
    expect(preferences.visibleColumnIds).not.toContain('cache_creation_tokens');
  });

  it('maps an existing v4 custom cache rate column while preserving order', () => {
    const preferences = normalizeRequestEventsPreferences({
      version: 4,
      pageSize: 100,
      visibleColumnIds: ['total_tokens', 'cache_rate', 'cache_creation_tokens', 'cache_read_tokens', 'timestamp'],
    });

    expect(preferences.visibleColumnIds).toEqual([
      'total_tokens',
      'cache_read_rate',
      'cache_creation_tokens',
      'cache_read_tokens',
      'timestamp',
    ]);
  });
});
