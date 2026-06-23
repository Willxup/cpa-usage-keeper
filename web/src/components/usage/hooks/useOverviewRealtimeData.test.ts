import { describe, expect, it } from 'vitest';
import type { OverviewRealtimeBlock, OverviewRealtimeWindow } from '@/lib/types';
import { resolveDisplayRealtime } from './useOverviewRealtimeData';

const realtimeForWindow = (window: OverviewRealtimeWindow): OverviewRealtimeBlock => ({
  window,
  bucket_seconds: window === '60m' ? 120 : window === '30m' ? 60 : 30,
  token_velocity: [],
  response_level: [],
  response_distribution: {
    ttft: { average_line: [], particles: [] },
    latency: { average_line: [], particles: [] },
  },
  current_usage: {
    models: [],
    api_keys: [],
    auth_files: [],
    ai_providers: [],
  },
  request_level: [],
  cache_level: [],
});

describe('resolveDisplayRealtime', () => {
  it('keeps the previous realtime block visible while a new window is loading', () => {
    expect(resolveDisplayRealtime({
      realtime: realtimeForWindow('15m'),
      lastRealtimeQueryKey: ':15m',
      realtimeQueryKey: ':60m',
    })?.window).toBe('15m');
  });

  it('keeps the previous realtime block visible before same-scope window loading starts', () => {
    expect(resolveDisplayRealtime({
      realtime: realtimeForWindow('15m'),
      lastRealtimeQueryKey: 'key-a:15m',
      realtimeQueryKey: 'key-a:60m',
    })?.window).toBe('15m');
  });

  it('hides stale realtime data after a same-scope window query fails', () => {
    expect(resolveDisplayRealtime({
      realtime: realtimeForWindow('15m'),
      lastRealtimeErrorQueryKey: 'key-a:60m',
      lastRealtimeQueryKey: 'key-a:15m',
      realtimeQueryKey: 'key-a:60m',
    })).toBeNull();
  });

  it('hides stale realtime data while loading if the API key changes', () => {
    expect(resolveDisplayRealtime({
      realtime: realtimeForWindow('15m'),
      lastRealtimeQueryKey: 'key-a:15m',
      realtimeQueryKey: 'key-b:15m',
    })).toBeNull();
  });

  it('hides stale realtime data before loading starts if the API key changes', () => {
    expect(resolveDisplayRealtime({
      realtime: realtimeForWindow('15m'),
      lastRealtimeQueryKey: 'key-a:15m',
      realtimeQueryKey: 'key-b:15m',
    })).toBeNull();
  });
});
