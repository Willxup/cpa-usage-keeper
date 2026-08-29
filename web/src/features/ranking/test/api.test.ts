import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  exitRanking,
  fetchLocalRankingLeaderboard,
  fetchRankingLeaderboard,
  fetchRankingMetadata,
  fetchRankingStatus,
  joinRanking,
  pauseRanking,
  RankingApiError,
  resumeRanking,
  syncRanking,
  updateLocalRankingProfile,
} from '../api';

const jsonResponse = (body: unknown, status = 200, headers?: HeadersInit) => new Response(
  JSON.stringify(body),
  { status, headers: { 'Content-Type': 'application/json', ...headers } },
);

describe('ranking API', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it('uses the Keeper base path and exact leaderboard selection', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: '/keeper/' });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse({
      period: 'today',
      period_key: '2026-07-24',
      metric: 'overall',
      generated_at: '2026-07-24T04:05:06Z',
      stale: false,
      entries: [],
    }));

    await fetchRankingLeaderboard('today', 'overall');

    const [rawURL, init] = fetchMock.mock.calls[0];
    const url = new URL(String(rawURL), 'http://localhost');
    expect(url.pathname).toBe('/keeper/api/v1/ranking/leaderboards');
    expect(url.search).toBe('?period=today&metric=overall');
    expect(init).toMatchObject({ credentials: 'include', cache: 'no-store' });
  });

  it('uses the dedicated local leaderboard endpoint', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: '/keeper/' });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse({
      period: 'today',
      period_key: '2026-07-31',
      metric: 'overall',
      generated_at: '2026-07-31T04:00:00Z',
      stale: false,
      entries: [],
    }));

    await fetchLocalRankingLeaderboard('today', 'overall');

    const [rawURL, init] = fetchMock.mock.calls[0];
    const url = new URL(String(rawURL), 'http://localhost');
    expect(url.pathname).toBe('/keeper/api/v1/ranking/local/leaderboards');
    expect(url.search).toBe('?period=today&metric=overall');
    expect(init).toMatchObject({ credentials: 'include', cache: 'no-store' });
  });

  it('updates a local Key profile through the dedicated admin endpoint', async () => {
	vi.stubGlobal('window', { __APP_BASE_PATH__: '/keeper/' });
	const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse({
		participant_id: '42',
		key_alias: 'Primary',
		display_name: 'Primary',
		avatar_id: 17,
	}));

	await updateLocalRankingProfile('42', { key_alias: 'Primary', avatar_id: 17 });

	const [rawURL, init] = fetchMock.mock.calls[0];
	expect(new URL(String(rawURL), 'http://localhost').pathname).toBe('/keeper/api/v1/ranking/local/profiles/42');
	expect(init).toMatchObject({ method: 'PATCH', credentials: 'include', cache: 'no-store' });
	expect(new Headers(init?.headers).get('X-CPA-Usage-Keeper-Request')).toBe('fetch');
	expect(init?.body).toBe(JSON.stringify({ key_alias: 'Primary', avatar_id: 17 }));
  });

  it('uses the local admin endpoints and request-intent header for every mutation', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation(async () => jsonResponse({ status: 'active' }));

    await fetchRankingStatus();
    await fetchRankingMetadata();
    await joinRanking({ display_name: 'Keeper_01', avatar_id: 7 });
    await syncRanking();
    await pauseRanking();
    await resumeRanking();
    await exitRanking();

    expect(fetchMock.mock.calls.map(([url, init]) => [
      new URL(String(url), 'http://localhost').pathname,
      init?.method ?? 'GET',
      new Headers(init?.headers).get('X-CPA-Usage-Keeper-Request'),
    ])).toEqual([
      ['/api/v1/ranking/status', 'GET', null],
      ['/api/v1/ranking/leaderboards/metadata', 'GET', null],
      ['/api/v1/ranking/join', 'POST', 'fetch'],
      ['/api/v1/ranking/sync', 'POST', 'fetch'],
      ['/api/v1/ranking/pause', 'POST', 'fetch'],
      ['/api/v1/ranking/resume', 'POST', 'fetch'],
      ['/api/v1/ranking', 'DELETE', 'fetch'],
    ]);
    expect(fetchMock.mock.calls[2]?.[1]?.body).toBe(JSON.stringify({ display_name: 'Keeper_01', avatar_id: 7 }));
  });

  it('preserves the server error code and Retry-After for actionable feedback', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse(
      { error: 'ranking_center_registration_rate_limited' },
      429,
      { 'Retry-After': '3599' },
    ));

    await expect(joinRanking({ display_name: 'Keeper_01', avatar_id: 7 })).rejects.toMatchObject({
      name: 'RankingApiError',
      status: 429,
      code: 'ranking_center_registration_rate_limited',
      retryAfter: '3599',
    } satisfies Partial<RankingApiError>);
  });
});
