import type { UsageActivityResponse } from '@/lib/types';

const ACTIVITY_BLOCK_COUNT = 7 * 52;

export const buildUsageActivityFixture = (tokenValues: readonly number[] = []): UsageActivityResponse => {
  const windowStart = Date.parse('2026-07-01T00:00:00Z');
  const windowEnd = Date.parse('2026-07-02T00:00:00Z');
  const bucketMilliseconds = (windowEnd - windowStart) / ACTIVITY_BLOCK_COUNT;
  const blocks = Array.from({ length: ACTIVITY_BLOCK_COUNT }, (_, index) => {
    const totalTokens = tokenValues[index] ?? 0;
    return {
      start_time: new Date(windowStart + index * bucketMilliseconds).toISOString(),
      end_time: new Date(windowStart + (index + 1) * bucketMilliseconds).toISOString(),
      success: index === ACTIVITY_BLOCK_COUNT - 1 ? 2 : 0,
      failure: index === ACTIVITY_BLOCK_COUNT - 1 ? 1 : 0,
      rate: index === ACTIVITY_BLOCK_COUNT - 1 ? 2 / 3 : -1,
      input_tokens: totalTokens > 0 ? Math.floor(totalTokens * 0.5) : 0,
      output_tokens: totalTokens > 0 ? Math.floor(totalTokens * 0.2) : 0,
      reasoning_tokens: totalTokens > 0 ? Math.floor(totalTokens * 0.1) : 0,
      cache_read_tokens: totalTokens > 0 ? Math.floor(totalTokens * 0.15) : 0,
      cache_creation_tokens: totalTokens > 0 ? Math.floor(totalTokens * 0.05) : 0,
      total_tokens: totalTokens,
    };
  });

  return {
    window: '24h',
    grain: 'short',
    timezone: 'UTC',
    total_success: 2,
    total_failure: 1,
    success_rate: (2 / 3) * 100,
    input_tokens: blocks.reduce((sum, block) => sum + block.input_tokens, 0),
    output_tokens: blocks.reduce((sum, block) => sum + block.output_tokens, 0),
    reasoning_tokens: blocks.reduce((sum, block) => sum + block.reasoning_tokens, 0),
    cache_read_tokens: blocks.reduce((sum, block) => sum + block.cache_read_tokens, 0),
    cache_creation_tokens: blocks.reduce((sum, block) => sum + block.cache_creation_tokens, 0),
    total_tokens: blocks.reduce((sum, block) => sum + block.total_tokens, 0),
    rows: 7,
    columns: 52,
    bucket_seconds: 238,
    window_start: new Date(windowStart).toISOString(),
    window_end: new Date(windowEnd).toISOString(),
    blocks,
  };
};

