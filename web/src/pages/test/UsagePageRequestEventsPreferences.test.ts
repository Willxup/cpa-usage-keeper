import { describe, expect, it } from 'vitest';
import {
  normalizeRequestEventsPreferences,
  type RequestEventsPreferences,
} from '../UsagePage';
import type { RequestEventColumnId } from '@/components/usage/RequestEventsDetailsCard';

const LEGACY_V2_FULL_COLUMNS = [
  'timestamp',
  'api_key',
  'source',
  'model',
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
] as unknown as RequestEventColumnId[];

const LEGACY_V1_FULL_COLUMNS_WITHOUT_SPEED_MODE_AND_MODEL_ALIAS = LEGACY_V2_FULL_COLUMNS.filter(
  (columnId) => columnId !== 'service_tier'
);

const EXPECTED_COLUMNS_WITH_MODEL_ALIAS = [
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

describe('UsagePage request event model alias preferences', () => {
  it('upgrades legacy v2 full-column preferences to include model alias', () => {
    const preferences = normalizeRequestEventsPreferences({
      version: 2,
      pageSize: 100,
      visibleColumnIds: LEGACY_V2_FULL_COLUMNS,
    });

    expect(preferences.version).toBe(3 as RequestEventsPreferences['version']);
    expect(preferences.visibleColumnIds).toEqual(EXPECTED_COLUMNS_WITH_MODEL_ALIAS);
  });

  it('keeps customized v2 preferences without model alias unchanged', () => {
    const customizedColumns = LEGACY_V2_FULL_COLUMNS.filter((columnId) => columnId !== 'speed');
    const preferences = normalizeRequestEventsPreferences({
      version: 2,
      pageSize: 100,
      visibleColumnIds: customizedColumns,
    });

    expect(preferences.visibleColumnIds).toEqual(customizedColumns);
    expect(preferences.visibleColumnIds).not.toContain('model_alias');
  });

  it('upgrades legacy v1 full-column preferences missing speed mode and model alias', () => {
    const preferences = normalizeRequestEventsPreferences({
      version: 1,
      pageSize: 100,
      visibleColumnIds: LEGACY_V1_FULL_COLUMNS_WITHOUT_SPEED_MODE_AND_MODEL_ALIAS,
    });

    expect(preferences.visibleColumnIds).toEqual(EXPECTED_COLUMNS_WITH_MODEL_ALIAS);
  });
});
