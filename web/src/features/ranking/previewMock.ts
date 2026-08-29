import type { RankingDataAPI } from './hooks/useRankingData';
import type { LocalRankingDataAPI } from './hooks/useLocalRankingData';
import type {
  LocalRankingProfileRequest,
  LocalRankingProfileResponse,
  RankingDetailMetric,
  RankingLeaderboardEntry,
  RankingLeaderboardResponse,
  RankingMetadataResponse,
  RankingMetric,
  RankingPeriod,
  RankingProfileRequest,
  RankingStatusResponse,
} from './types';

const ACTIVE_PROFILE: RankingStatusResponse = {
  status: 'active',
  participant_id: 'preview-owner',
  display_name: 'KeeperNovaMaster',
  avatar_id: 12,
  last_successful_complete_day: '2026-07-24',
  last_successful_sync_at: '2026-07-25T06:20:00Z',
  last_attempt_at: '2026-07-25T06:20:00Z',
};

const DETAIL_METRICS: RankingDetailMetric[] = [
  'total_tokens',
  'request_count',
  'cache_read_rate',
  'ttft_average',
  'latency_average',
  'peak_tpm',
  'peak_rpm',
];

const PERIOD_KEYS: Record<RankingPeriod, string> = {
  today: '2026-07-25',
  yesterday: '2026-07-24',
  current_month: '2026-07',
  previous_month: '2026-06',
};

const PERIOD_FACTORS: Record<RankingPeriod, number> = {
  today: 0.082,
  yesterday: 0.091,
  current_month: 1,
  previous_month: 0.92,
};

const PARTICIPANT_NAMES = [
  'KeeperNovaMaster',
  'TokenPilot',
  'GridRunner',
  'ByteSky',
  'CacheFox',
  'FastTTFT',
  'ModelForge',
  'RedCircuit',
  'CloudMint',
  'PromptCat',
  'ApiRanger',
  'NightOwl',
  'DataPulse',
  'CoreSpark',
  'TokenWave',
  'BytePilot',
] as const;

const PARTICIPANT_AVATARS = [12, 7, 3, 45, 22, 61, 30, 5, 52, 18, 34, 66, 9, 41, 27, 57] as const;

interface PreviewParticipant {
  participantID: string;
  displayName: string;
  keyAlias?: string;
  fallbackDisplayName?: string;
  avatarID: number;
  score: number;
  metrics: Record<RankingDetailMetric, number>;
}

const PARTICIPANTS: PreviewParticipant[] = PARTICIPANT_NAMES.map((displayName, index) => ({
  participantID: index === 0 ? 'preview-owner' : `preview-${index + 1}`,
  displayName,
  avatarID: PARTICIPANT_AVATARS[index]!,
  score: 9_680 - index * 97,
  metrics: {
    total_tokens: 1_840_000_000 - index * 79_000_000,
    request_count: 740_000 - index * 31_000,
    cache_read_rate: 920_000 - index * 14_000,
    ttft_average: 165_000 + index * 17_000,
    latency_average: 1_350_000 + index * 115_000,
    peak_tpm: 2_800_000 - index * 95_000,
    peak_rpm: 7_600 - index * 240,
  },
}));

const isLowerBetter = (metric: RankingMetric) => metric === 'ttft_average' || metric === 'latency_average';

const scaleMetric = (metric: RankingDetailMetric, value: number, factor: number): number => {
  if (metric === 'cache_read_rate' || metric === 'ttft_average' || metric === 'latency_average') {
    return value;
  }
  return Math.round(value * factor);
};

const buildEntry = (
  participant: PreviewParticipant,
  period: RankingPeriod,
  metric: RankingMetric,
): RankingLeaderboardEntry => {
  const factor = PERIOD_FACTORS[period];
  const metrics = Object.fromEntries(
    DETAIL_METRICS.map((item) => [item, scaleMetric(item, participant.metrics[item], factor)]),
  ) as Record<RankingDetailMetric, number>;
  const periodScoreOffset = period === 'current_month' ? 20 : period === 'previous_month' ? -35 : 0;
  const displayName = participant.keyAlias === undefined
    ? participant.displayName
    : participant.keyAlias || participant.fallbackDisplayName || participant.displayName;
  return {
    rank: 0,
    participant_id: participant.participantID,
    display_name: displayName,
    avatar_id: participant.avatarID,
    value: metric === 'overall' ? participant.score + periodScoreOffset : metrics[metric],
    metrics,
  };
};

const buildLeaderboard = (
  period: RankingPeriod,
  metric: RankingMetric,
  local = false,
  participants: PreviewParticipant[] = PARTICIPANTS,
): RankingLeaderboardResponse => {
  const direction = isLowerBetter(metric) ? 1 : -1;
  const entries = participants
    .map((participant) => {
      const entry = buildEntry(participant, period, metric);
      const localEntry = local ? { ...entry, key_alias: participant.keyAlias ?? '' } : entry;
      return local && metric === 'overall' ? { ...localEntry, value: Math.round(entry.value / 100) } : localEntry;
    })
    .sort((left, right) => (left.value - right.value) * direction)
    .map((entry, index) => ({ ...entry, rank: index + 1 }));
  return {
    period,
    period_key: PERIOD_KEYS[period],
    metric,
    generated_at: new Date().toISOString(),
    stale: false,
    entries,
  };
};

export const createLocalRankingPreviewAPI = (): LocalRankingDataAPI => {
  const participants = PARTICIPANTS.map((participant, index) => ({
    ...participant,
    keyAlias: participant.displayName,
    fallbackDisplayName: `sk-*********${String(index + 1).padStart(4, '0')}`,
    metrics: { ...participant.metrics },
  }));
  return {
    leaderboard: async (period, metric) => buildLeaderboard(period, metric, true, participants),
    updateProfile: async (participantID: string, profile: LocalRankingProfileRequest): Promise<LocalRankingProfileResponse> => {
      const participant = participants.find((item) => item.participantID === participantID);
      if (!participant) throw new Error('local ranking preview profile not found');
      participant.keyAlias = profile.key_alias.trim();
      participant.avatarID = profile.avatar_id;
      const displayName = participant.keyAlias || participant.fallbackDisplayName || participant.displayName;
      return {
        participant_id: participantID,
        key_alias: participant.keyAlias,
        display_name: displayName,
        avatar_id: participant.avatarID,
      };
    },
  };
};

const buildMetadata = (): RankingMetadataResponse => ({
  server_time: new Date().toISOString(),
  generated_at: new Date().toISOString(),
  stale: false,
  protocol_version: 1,
  metrics_version: 1,
  period_timezone: 'Asia/Shanghai',
  avatar_catalog_version: 1,
  avatar_count: 66,
  read_marker_version: 1,
  refresh_interval_seconds: 60,
  suggested_sync_interval_seconds: 1800,
  periods: (Object.keys(PERIOD_KEYS) as RankingPeriod[]).map((period) => ({
    period,
    period_key: PERIOD_KEYS[period],
    online: true,
  })),
  metrics: ['overall', ...DETAIL_METRICS],
  overall_weights: {
    total_tokens: 25,
    request_count: 15,
    cache_read_rate: 15,
    ttft_average: 15,
    latency_average: 10,
    peak_tpm: 10,
    peak_rpm: 10,
  },
});

const cloneStatus = (status: RankingStatusResponse): RankingStatusResponse => ({ ...status });

export const createRankingPreviewAPI = (): RankingDataAPI => {
  let status = cloneStatus(ACTIVE_PROFILE);
  return {
    status: async () => cloneStatus(status),
    metadata: async () => buildMetadata(),
    leaderboard: async (period, metric) => buildLeaderboard(period, metric),
    join: async (profile: RankingProfileRequest) => {
      status = {
        ...ACTIVE_PROFILE,
        display_name: profile.display_name,
        avatar_id: profile.avatar_id,
        last_successful_sync_at: new Date().toISOString(),
      };
      return cloneStatus(status);
    },
    sync: async () => {
      status = {
        ...status,
        last_attempt_at: new Date().toISOString(),
        last_successful_sync_at: new Date().toISOString(),
        last_error: undefined,
      };
      return cloneStatus(status);
    },
    pause: async () => {
      status = { ...status, status: 'paused' };
      return cloneStatus(status);
    },
    resume: async () => {
      status = { ...status, status: 'active' };
      return cloneStatus(status);
    },
    exit: async () => {
      status = { ...status, status: 'deleted' };
      return cloneStatus(status);
    },
  };
};

let previewAPI: RankingDataAPI | undefined;
let localPreviewAPI: LocalRankingDataAPI | undefined;

export const resolveRankingPreviewAPI = (enabled?: string): RankingDataAPI | undefined => {
  if (enabled !== 'true') return undefined;
  previewAPI ??= createRankingPreviewAPI();
  return previewAPI;
};

export const resolveLocalRankingPreviewAPI = (enabled?: string): LocalRankingDataAPI | undefined => {
  if (enabled !== 'true') return undefined;
  localPreviewAPI ??= createLocalRankingPreviewAPI();
  return localPreviewAPI;
};
