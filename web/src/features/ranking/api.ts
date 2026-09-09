import { apiFetch, apiPath } from '@/lib/api';
import type {
  LocalRankingProfileRequest,
  LocalRankingProfileResponse,
  RankingLeaderboardResponse,
  RankingMetadataResponse,
  RankingMetric,
  RankingPeriod,
  RankingProfileRequest,
  RankingStatusResponse,
} from './types';

export class RankingApiError extends Error {
  status: number;
  code: string;
  retryAfter: string;

  constructor(code: string, status: number, retryAfter = '') {
    super(code);
    this.name = 'RankingApiError';
    this.status = status;
    this.code = code;
    this.retryAfter = retryAfter;
  }
}

const requestRankingJSON = async <T>(path: string, init: RequestInit = {}): Promise<T> => {
  const headers = new Headers(init.headers);
  const method = (init.method ?? 'GET').toUpperCase();
  if (method !== 'GET' && method !== 'HEAD') {
    headers.set('X-CPA-Usage-Keeper-Request', 'fetch');
  }
  const response = await apiFetch(apiPath(path), {
    ...init,
    credentials: 'include',
    cache: 'no-store',
    headers,
  });
  if (!response.ok) {
    let code = 'ranking_request_failed';
    try {
      const payload = await response.json() as { error?: string };
      code = payload.error?.trim() || code;
    } catch {
      // 非 JSON 错误仍保留稳定的本地错误码。
    }
    throw new RankingApiError(code, response.status, response.headers.get('Retry-After') ?? '');
  }
  return response.json() as Promise<T>;
};

export const fetchRankingStatus = (signal?: AbortSignal) => requestRankingJSON<RankingStatusResponse>(
  '/ranking/status',
  { signal },
);

export const joinRanking = (profile: RankingProfileRequest, signal?: AbortSignal) => requestRankingJSON<RankingStatusResponse>(
  '/ranking/join',
  {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(profile),
    signal,
  },
);

export const syncRanking = (signal?: AbortSignal) => requestRankingJSON<RankingStatusResponse>(
  '/ranking/sync',
  { method: 'POST', signal },
);

export const pauseRanking = (signal?: AbortSignal) => requestRankingJSON<RankingStatusResponse>(
  '/ranking/pause',
  { method: 'POST', signal },
);

export const resumeRanking = (signal?: AbortSignal) => requestRankingJSON<RankingStatusResponse>(
  '/ranking/resume',
  { method: 'POST', signal },
);

export const exitRanking = (signal?: AbortSignal) => requestRankingJSON<RankingStatusResponse>(
  '/ranking',
  { method: 'DELETE', signal },
);

export const fetchRankingLeaderboard = (
  period: RankingPeriod,
  metric: RankingMetric,
  signal?: AbortSignal,
) => {
  const query = new URLSearchParams({ period, metric });
  return requestRankingJSON<RankingLeaderboardResponse>(`/ranking/leaderboards?${query.toString()}`, { signal });
};

export const fetchLocalRankingLeaderboard = (
  period: RankingPeriod,
  metric: RankingMetric,
  signal?: AbortSignal,
) => {
  const query = new URLSearchParams({ period, metric });
  return requestRankingJSON<RankingLeaderboardResponse>(`/ranking/local/leaderboards?${query.toString()}`, { signal });
};

export const fetchKeyRankingLeaderboard = (
  period: RankingPeriod,
  metric: RankingMetric,
  signal?: AbortSignal,
) => {
  const query = new URLSearchParams({ period, metric });
  return requestRankingJSON<RankingLeaderboardResponse>(`/key-ranking/leaderboards?${query.toString()}`, { signal });
};

export const fetchKeyLocalRankingLeaderboard = (
  period: RankingPeriod,
  metric: RankingMetric,
  signal?: AbortSignal,
) => {
  const query = new URLSearchParams({ period, metric });
  return requestRankingJSON<RankingLeaderboardResponse>(`/key-ranking/local/leaderboards?${query.toString()}`, { signal });
};

export const updateLocalRankingProfile = (
  participantID: string,
  profile: LocalRankingProfileRequest,
  signal?: AbortSignal,
) => requestRankingJSON<LocalRankingProfileResponse>(
  `/ranking/local/profiles/${encodeURIComponent(participantID)}`,
  {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(profile),
    signal,
  },
);

export const fetchRankingMetadata = (signal?: AbortSignal) => requestRankingJSON<RankingMetadataResponse>(
  '/ranking/leaderboards/metadata',
  { signal },
);
