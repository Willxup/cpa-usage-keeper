import { describe, expect, it } from 'vitest';
import { createLocalRankingPreviewAPI, createRankingPreviewAPI, resolveLocalRankingPreviewAPI, resolveRankingPreviewAPI } from '../previewMock';

describe('Ranking preview mock', () => {
  it('stays disabled unless the local preview build explicitly enables it', () => {
    expect(resolveRankingPreviewAPI(undefined)).toBeUndefined();
    expect(resolveRankingPreviewAPI('false')).toBeUndefined();
    expect(resolveLocalRankingPreviewAPI('false')).toBeUndefined();
    expect(resolveRankingPreviewAPI('true')).toBeDefined();
  });

  it('uses the 0-100 score scale for local preview boards', async () => {
    const api = resolveLocalRankingPreviewAPI('true');
    const board = await api?.leaderboard('today', 'overall');
    expect(board?.entries[0]?.value).toBeLessThanOrEqual(100);
  });

  it('keeps local preview alias and avatar edits across leaderboard reloads', async () => {
    const api = createLocalRankingPreviewAPI();
    const before = await api.leaderboard('today', 'overall');
    const participantID = before.entries[0]!.participant_id;
    await api.updateProfile?.(participantID, { key_alias: '', avatar_id: 42 });
    const after = await api.leaderboard('today', 'overall');

    expect(after.entries[0]).toMatchObject({
      participant_id: participantID,
      key_alias: '',
      avatar_id: 42,
    });
    expect(after.entries[0]?.display_name).toMatch(/^sk-\*+/);
  });

  it('provides an active profile and complete leaderboard data for visual testing', async () => {
    const api = createRankingPreviewAPI();

    const status = await api.status();
    const metadata = await api.metadata();
    const overall = await api.leaderboard('today', 'overall');
    const latency = await api.leaderboard('current_month', 'latency_average');

    expect(status).toMatchObject({
      status: 'active',
      display_name: 'KeeperNovaMaster',
      avatar_id: 12,
    });
    expect([...(status.display_name ?? '')]).toHaveLength(16);
    expect(metadata.periods).toHaveLength(4);
    expect(metadata.periods.every((period) => period.online)).toBe(true);
    expect(metadata.metrics).toHaveLength(8);
    expect(overall.entries.length).toBeGreaterThanOrEqual(12);
    expect(overall.entries.map((entry) => entry.rank)).toEqual(
      overall.entries.map((_, index) => index + 1),
    );
    expect(overall.entries.some((entry) => entry.display_name === status.display_name)).toBe(true);
    expect(Object.keys(overall.entries[0]?.metrics ?? {})).toHaveLength(7);
    expect(latency.entries.every((entry, index, entries) => (
      index === 0 || entries[index - 1]!.value <= entry.value
    ))).toBe(true);
  });

  it('previews the local pause and resume lifecycle without changing the profile', async () => {
    const api = createRankingPreviewAPI();
    const paused = await api.pause();
    const resumed = await api.resume();

    expect(paused).toMatchObject({ status: 'paused', display_name: 'KeeperNovaMaster', avatar_id: 12 });
    expect(resumed).toMatchObject({ status: 'active', display_name: 'KeeperNovaMaster', avatar_id: 12 });
  });
});
