// @vitest-environment happy-dom

import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { RankingApiError } from '@/features/ranking/api';
import type { RankingLeaderboardResponse, RankingMetric, RankingPeriod, RankingScope } from '@/features/ranking/types';

globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const apiMocks = vi.hoisted(() => ({
  fetchKeyRankingLeaderboard: vi.fn(),
  fetchKeyLocalRankingLeaderboard: vi.fn(),
  renderedResults: [] as Array<{ scope: RankingScope; metric: string; names: string }>,
}));

vi.mock('@/features/ranking/api', async (importOriginal) => ({
  ...await importOriginal<typeof import('@/features/ranking/api')>(),
  fetchKeyRankingLeaderboard: apiMocks.fetchKeyRankingLeaderboard,
  fetchKeyLocalRankingLeaderboard: apiMocks.fetchKeyLocalRankingLeaderboard,
}));

vi.mock('@/features/key-viewer/KeyViewerShell', () => ({
  KeyViewerShell: ({ children, toolbar, activePage }: {
    children: React.ReactNode;
    toolbar: React.ReactNode;
    activePage: string;
  }) => <div data-active-page={activePage}>{toolbar}{children}</div>,
}));

vi.mock('@/features/ranking/components/RankingScopeSwitch', () => ({
  RankingScopeSwitch: ({ value, onChange }: { value: RankingScope; onChange: (scope: RankingScope) => void }) => (
    <div data-current-scope={value}>
      <button type="button" data-scope="local" onClick={() => onChange('local')}>local</button>
      <button type="button" data-scope="community" onClick={() => onChange('community')}>community</button>
    </div>
  ),
}));

vi.mock('@/features/ranking/components/RankingToolbar', () => ({
  RankingMetricSelect: ({ metric, onMetricChange }: {
    metric: RankingMetric;
    onMetricChange: (metric: RankingMetric) => void;
  }) => <button type="button" data-ranking-metric onClick={() => onMetricChange('total_tokens')}>{metric}</button>,
  RankingToolbar: ({ period, onPeriodChange }: {
    period: RankingPeriod;
    onPeriodChange: (period: RankingPeriod) => void;
  }) => <button type="button" data-ranking-period onClick={() => onPeriodChange('yesterday')}>{period}</button>,
}));

vi.mock('@/features/ranking/components/RankingLeaderboardResults', () => ({
  RankingLeaderboardResults: ({ scope, metric, entries, onEditLocalProfile }: {
    scope: RankingScope;
    metric: string;
    entries: Array<{ display_name: string }>;
    onEditLocalProfile?: () => void;
  }) => {
    const names = entries.map((entry) => entry.display_name).join(',');
    apiMocks.renderedResults.push({ scope, metric, names });
    return (
      <div data-ranking-results data-editable={String(Boolean(onEditLocalProfile))}>
        {names}
      </div>
    );
  },
}));

vi.mock('@/components/ui/MainActionButton', () => ({
  MainActionButton: ({ children, loading: _loading, shellClassName: _shellClassName, ...props }: React.ButtonHTMLAttributes<HTMLButtonElement> & {
    loading?: boolean;
    shellClassName?: string;
  }) => <button {...props}>{children}</button>,
}));

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key, i18n: { language: 'en' } }),
}));

import { KeyRankingPage } from '../KeyRankingPage';

type Deferred<T> = {
  promise: Promise<T>;
  resolve: (value: T) => void;
};

const deferred = <T,>(): Deferred<T> => {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((nextResolve) => {
    resolve = nextResolve;
  });
  return { promise, resolve };
};

const board = (displayName: string): RankingLeaderboardResponse => ({
  period: 'today',
  period_key: '2026-08-28',
  metric: 'overall',
  generated_at: '2026-08-28T04:00:00Z',
  stale: false,
  entries: [{
    rank: 1,
    participant_id: displayName,
    display_name: displayName,
    avatar_id: 1,
    value: 100,
  }],
});

describe('KeyRankingPage', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
    apiMocks.fetchKeyRankingLeaderboard.mockReset();
    apiMocks.fetchKeyLocalRankingLeaderboard.mockReset();
    apiMocks.renderedResults.length = 0;
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
  });

  it('defaults to a read-only Community board and hides Local scope when disabled', async () => {
    apiMocks.fetchKeyRankingLeaderboard.mockResolvedValue(board('Community'));

    await act(async () => {
      root.render(<KeyRankingPage apiKey={{ display_key: 'sk-***', local_ranking_enabled: false }} onNavigate={() => {}} />);
      await Promise.resolve();
    });

    expect(apiMocks.fetchKeyRankingLeaderboard).toHaveBeenCalledWith('today', 'overall', expect.any(AbortSignal));
    expect(apiMocks.fetchKeyLocalRankingLeaderboard).not.toHaveBeenCalled();
    expect(container.querySelector('[data-scope="local"]')).toBeNull();
    expect(container.querySelector('[data-ranking-results]')?.textContent).toBe('Community');
    expect(container.querySelector('[data-ranking-results]')?.getAttribute('data-editable')).toBe('false');
  });

  it('loads Local only when enabled and ignores the superseded Community response', async () => {
    const community = deferred<RankingLeaderboardResponse>();
    apiMocks.fetchKeyRankingLeaderboard.mockReturnValue(community.promise);
    apiMocks.fetchKeyLocalRankingLeaderboard.mockResolvedValue(board('Local'));

    await act(async () => {
      root.render(<KeyRankingPage apiKey={{ display_key: 'sk-***', local_ranking_enabled: true }} onNavigate={() => {}} />);
    });
    await act(async () => {
      container.querySelector<HTMLButtonElement>('[data-scope="local"]')?.click();
      await Promise.resolve();
    });

    expect(apiMocks.fetchKeyLocalRankingLeaderboard).toHaveBeenCalledWith('today', 'overall', expect.any(AbortSignal));
    expect(container.querySelector('[data-ranking-results]')?.textContent).toBe('Local');

    await act(async () => {
      community.resolve(board('Old Community'));
      await Promise.resolve();
    });
    expect(container.querySelector('[data-ranking-results]')?.textContent).toBe('Local');
  });

  it('returns to authentication when the Viewer ranking endpoint rejects the session', async () => {
    apiMocks.fetchKeyRankingLeaderboard.mockRejectedValue(new RankingApiError('expired', 401));
    const onAuthRequired = vi.fn();

    await act(async () => {
      root.render(<KeyRankingPage onNavigate={() => {}} onAuthRequired={onAuthRequired} />);
      await Promise.resolve();
    });

    expect(onAuthRequired).toHaveBeenCalled();
  });

  it('keeps the current board visible when a refresh fails', async () => {
    apiMocks.fetchKeyRankingLeaderboard.mockResolvedValueOnce(board('Community'));

    await act(async () => {
      root.render(<KeyRankingPage onNavigate={() => {}} />);
      await Promise.resolve();
    });
    apiMocks.fetchKeyRankingLeaderboard.mockRejectedValueOnce(new RankingApiError('temporary', 503));

    await act(async () => {
      Array.from(container.querySelectorAll<HTMLButtonElement>('button'))
        .find((button) => button.textContent?.includes('usage_stats.refresh'))
        ?.click();
      await Promise.resolve();
    });

    expect(container.querySelector('[role="alert"]')).not.toBeNull();
    expect(container.querySelector('[data-ranking-results]')?.textContent).toBe('Community');
  });

  it('removes the current Community board when the selected period is no longer public', async () => {
    apiMocks.fetchKeyRankingLeaderboard.mockResolvedValueOnce(board('Community'));

    await act(async () => {
      root.render(<KeyRankingPage onNavigate={() => {}} />);
      await Promise.resolve();
    });
    apiMocks.fetchKeyRankingLeaderboard.mockRejectedValueOnce(
      new RankingApiError('ranking_center_leaderboard_unavailable', 404),
    );

    await act(async () => {
      Array.from(container.querySelectorAll<HTMLButtonElement>('button'))
        .find((button) => button.textContent?.includes('usage_stats.refresh'))
        ?.click();
      await Promise.resolve();
    });

    expect(container.querySelector('[role="alert"]')).not.toBeNull();
    expect(container.querySelector('[data-ranking-results]')).toBeNull();
  });

  it('does not render the previous board under a newly selected scope', async () => {
    apiMocks.fetchKeyRankingLeaderboard.mockResolvedValueOnce(board('Community'));
    apiMocks.fetchKeyLocalRankingLeaderboard.mockReturnValue(deferred<RankingLeaderboardResponse>().promise);

    await act(async () => {
      root.render(<KeyRankingPage apiKey={{ display_key: 'sk-***', local_ranking_enabled: true }} onNavigate={() => {}} />);
      await Promise.resolve();
    });
    apiMocks.renderedResults.length = 0;

    await act(async () => {
      container.querySelector<HTMLButtonElement>('[data-scope="local"]')?.click();
      await Promise.resolve();
    });

    expect(apiMocks.renderedResults).not.toContainEqual({
      scope: 'local',
      metric: 'overall',
      names: 'Community',
    });
  });

  it('does not render the previous board under a newly selected metric', async () => {
    apiMocks.fetchKeyRankingLeaderboard.mockResolvedValueOnce(board('Community'));

    await act(async () => {
      root.render(<KeyRankingPage onNavigate={() => {}} />);
      await Promise.resolve();
    });
    apiMocks.fetchKeyRankingLeaderboard.mockReturnValueOnce(deferred<RankingLeaderboardResponse>().promise);
    apiMocks.renderedResults.length = 0;

    await act(async () => {
      container.querySelector<HTMLButtonElement>('[data-ranking-metric]')?.click();
      await Promise.resolve();
    });

    expect(apiMocks.renderedResults).not.toContainEqual({
      scope: 'community',
      metric: 'total_tokens',
      names: 'Community',
    });
  });

  it('does not render the previous board while a newly selected period loads', async () => {
    apiMocks.fetchKeyRankingLeaderboard.mockResolvedValueOnce(board('Community'));

    await act(async () => {
      root.render(<KeyRankingPage onNavigate={() => {}} />);
      await Promise.resolve();
    });
    apiMocks.fetchKeyRankingLeaderboard.mockReturnValueOnce(deferred<RankingLeaderboardResponse>().promise);
    apiMocks.renderedResults.length = 0;

    await act(async () => {
      container.querySelector<HTMLButtonElement>('[data-ranking-period]')?.click();
      await Promise.resolve();
    });

    expect(apiMocks.renderedResults).toEqual([]);
  });
});
