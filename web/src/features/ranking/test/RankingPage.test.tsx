// @vitest-environment happy-dom

import { act, useState } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { RankingApiError } from '../api';
import { RankingPage } from '../RankingPage';
import type { RankingLeaderboardResponse, RankingStatusResponse } from '../types';

globalThis.IS_REACT_ACT_ENVIRONMENT = true;

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, params?: Record<string, string | number>) => params ? `${key}:${JSON.stringify(params)}` : key,
    i18n: { language: 'en' },
  }),
}));

const leaderboard: RankingLeaderboardResponse = {
  period: 'today',
  period_key: '2026-07-24',
  metric: 'overall',
  generated_at: '2026-07-24T04:05:00Z',
  stale: false,
  score_explanation: {
    version: 2,
    texts: {
      en: 'Overall score V2 from the ranking center.',
      zh: '中心返回的综合分 V2。',
      'zh-TW': '中心回傳的綜合分 V2。',
    },
  },
  entries: [
    {
      rank: 4,
      participant_id: 'p_secret_1',
      display_name: 'SameName',
      avatar_id: 1,
      value: 9_325,
      metrics: {
        total_tokens: 1_500,
        request_count: 250,
        cache_read_rate: 825_000,
        ttft_average: 125_000,
        latency_average: 2_500_000,
        peak_tpm: 5_000,
        peak_rpm: 45,
      },
    },
    {
      rank: 9,
      participant_id: 'p_secret_2',
      display_name: 'SameName',
      avatar_id: 2,
      value: 9_100,
      metrics: {},
    },
    {
      rank: 12,
      participant_id: 'p_secret_3',
      display_name: 'ThirdName',
      avatar_id: 3,
      value: 8_900,
      metrics: {},
    },
    {
      rank: 14,
      participant_id: 'p_secret_4',
      display_name: 'FourthName',
      avatar_id: 4,
      value: 8_700,
      metrics: {},
    },
    {
      rank: 18,
      participant_id: 'p_secret_5',
      display_name: 'FifthName',
      avatar_id: 5,
      value: 8_500,
      metrics: {},
    },
  ],
};

const defaultProps = {
  scope: 'community' as const,
  period: 'today' as const,
  metric: 'overall' as const,
  status: { status: 'disabled' } as RankingStatusResponse,
  metadata: null,
  leaderboard,
  statusLoading: false,
  metadataLoading: false,
  leaderboardLoading: false,
  statusError: null,
  metadataError: null,
  leaderboardError: null,
  action: null,
  actionError: null,
  onClearActionError: vi.fn(),
  onJoin: vi.fn(async () => null),
  onSync: vi.fn(async () => null),
  onPause: vi.fn(async () => null),
  onResume: vi.fn(async () => null),
  onExit: vi.fn(async () => null),
  onRetryStatus: vi.fn(async () => null),
  onRetryMetadata: vi.fn(async () => null),
  onRetryLeaderboard: vi.fn(async () => null),
  onUpdateLocalProfile: vi.fn(async (participantID: string, profile: { key_alias: string; avatar_id: number }) => ({
    participant_id: participantID,
    key_alias: profile.key_alias,
    display_name: profile.key_alias,
    avatar_id: profile.avatar_id,
  })),
  onPeriodChange: vi.fn(),
  onMetricChange: vi.fn(),
};

describe('RankingPage', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
    vi.clearAllMocks();
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    document.body.replaceChildren();
  });

  const renderPage = async (overrides = {}) => {
    await act(async () => {
      root.render(<RankingPage {...defaultProps} {...overrides} />);
    });
  };

  const click = async (selector: string, scope: ParentNode = document) => {
    await act(async () => scope.querySelector<HTMLButtonElement>(selector)!.click());
  };

  const setInput = async (input: HTMLInputElement, value: string) => {
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value);
      input.dispatchEvent(new Event('input', { bubbles: true }));
    });
  };

  const openProfileModal = async () => {
    await click('[data-ranking-profile-action]', container);
    // Modal 会在下一任务设置初始焦点，等待完成后再让用例操作弹窗内焦点。
    await act(async () => {
      await new Promise<void>((resolve) => {
        window.setTimeout(resolve, 0);
      });
    });
  };

  it('renders a labelled heading with metric, period, and community profile controls', async () => {
    await renderPage();

    const title = container.querySelector('[data-ranking-header-title]')!;
    const toolbar = container.querySelector('[data-ranking-header-toolbar]')!;
    expect(container.querySelector('[data-ranking-profile-action]')).not.toBeNull();
    expect(title?.querySelector('[data-ranking-metric-title]')).not.toBeNull();
    expect(title?.querySelector('[data-ranking-score-explanation-slot]')).not.toBeNull();
    expect(title?.querySelector('[role="heading"][aria-level="2"]')).not.toBeNull();
    expect(toolbar?.querySelector('[data-ranking-toolbar]')).not.toBeNull();
    expect(toolbar?.querySelector('[data-ranking-period]')).not.toBeNull();
    expect(title?.textContent).toContain('ranking.metric_short_overall');
    expect(title?.textContent).toContain('ranking.period_trigger_today');
  });

  it('removes the participation entry entirely from the local leaderboard', async () => {
    await renderPage({
      scope: 'local',
      status: { status: 'active', display_name: 'Owner', avatar_id: 7 },
      leaderboard: {
        ...leaderboard,
        entries: leaderboard.entries.map((entry) => ({ ...entry, value: Math.round(entry.value / 100) })),
      },
    });

    expect(container.querySelector('[data-ranking-profile-action-shell]')).toBeNull();
    expect(container.querySelector('[data-ranking-profile-action]')).toBeNull();
    expect(container.querySelector('[data-ranking-participant-column]')?.textContent).toBe('ranking.api_key');
    expect(container.textContent).toContain('93 PTS');
    expect(container.textContent).not.toContain('93.25 PTS');
  });

  it('opens the same local Key editor from podium and table avatars and saves alias with avatar', async () => {
    const onUpdateLocalProfile = defaultProps.onUpdateLocalProfile;
    const localLeaderboard: RankingLeaderboardResponse = {
      ...leaderboard,
      entries: leaderboard.entries.map((entry, index) => ({
        ...entry,
        participant_id: String(index + 1),
        display_name: index === 0 ? 'Primary' : entry.display_name,
        key_alias: index === 0 ? 'Primary' : '',
        value: Math.round(entry.value / 100),
      })),
    };
    await renderPage({ scope: 'local', leaderboard: localLeaderboard, onUpdateLocalProfile });

    expect(container.querySelectorAll('[data-ranking-podium] [data-ranking-local-profile-edit]')).toHaveLength(3);
    expect(container.querySelectorAll('tbody [data-ranking-local-profile-edit]')).toHaveLength(5);
    await click('[data-ranking-podium-rank="1"] [data-ranking-local-profile-edit]', container);

    const aliasInput = document.querySelector<HTMLInputElement>('input[name="local-ranking-key-alias"]');
    expect(aliasInput?.value).toBe('Primary');
    expect(aliasInput?.placeholder).toBe('');
    await setInput(aliasInput!, 'Renamed');
    await click('[data-ranking-avatar-option="42"]');
    await click('[data-ranking-local-profile-save]');

    expect(onUpdateLocalProfile).toHaveBeenCalledWith('1', { key_alias: 'Renamed', avatar_id: 42 });
  });

  it('uses the masked Key as the placeholder only when no alias exists', async () => {
    const localLeaderboard: RankingLeaderboardResponse = {
      ...leaderboard,
      entries: leaderboard.entries.map((entry, index) => ({
        ...entry,
        participant_id: String(index + 1),
        display_name: index === 0 ? 'sk-*********alpha' : entry.display_name,
        key_alias: '',
      })),
    };
    await renderPage({ scope: 'local', leaderboard: localLeaderboard });
    await click('[data-ranking-podium-rank="1"] [data-ranking-local-profile-edit]', container);

    const aliasInput = document.querySelector<HTMLInputElement>('input[name="local-ranking-key-alias"]');
    expect(aliasInput?.value).toBe('');
    expect(aliasInput?.placeholder).toBe('sk-*********alpha');
  });

  it('keeps Community leaderboard avatars non-editable', async () => {
    await renderPage();
    expect(container.querySelector('[data-ranking-local-profile-edit]')).toBeNull();
  });

  it('shows the center explanation beside only a V2 overall title', async () => {
    await renderPage();

    const hint = container.querySelector<HTMLButtonElement>('[data-ranking-score-explanation]');
    const descriptionID = hint?.getAttribute('aria-describedby');
    const description = descriptionID ? document.getElementById(descriptionID) : null;
    const tooltipID = hint?.getAttribute('aria-controls');
    const tooltip = tooltipID ? document.getElementById(tooltipID) : null;
    expect(hint?.textContent).toContain('?');
    expect(hint?.getAttribute('title')).toBeNull();
    expect(hint?.getAttribute('aria-label')).toBe('ranking.score_explanation_label');
    expect(hint?.getAttribute('aria-label')).not.toBe(tooltip?.textContent);
    expect(description?.textContent).toBe('Overall score V2 from the ranking center.');
    expect(hint?.getAttribute('aria-controls')).toBe(tooltip?.id);
    expect(hint?.getAttribute('aria-expanded')).toBe('false');
    expect(tooltip?.getAttribute('role')).toBe('tooltip');
    expect(tooltip?.textContent).toBe('Overall score V2 from the ranking center.');
    await act(async () => hint!.focus());
    expect(hint?.getAttribute('aria-expanded')).toBe('true');
    await act(async () => hint!.blur());
    expect(hint?.getAttribute('aria-expanded')).toBe('false');

    await renderPage({
      metric: 'total_tokens',
      leaderboard: { ...leaderboard, metric: 'total_tokens' },
    });
    expect(container.querySelector('[data-ranking-score-explanation]')).toBeNull();

    await renderPage({ leaderboard: { ...leaderboard, score_explanation: undefined } });
    expect(container.querySelector('[data-ranking-score-explanation]')).toBeNull();
    expect(container.querySelector('[data-ranking-score-explanation-slot]')).toBeNull();

    await renderPage({
      leaderboard: { ...leaderboard, score_explanation: { version: 2, texts: null } },
    });
    expect(container.querySelector('[data-ranking-score-explanation]')).toBeNull();

    await renderPage({
      leaderboard: { ...leaderboard, score_explanation: { version: 1, texts: { en: 'Legacy score' } } },
    });
    expect(container.querySelector('[data-ranking-score-explanation]')).toBeNull();
  });

  it('keeps the overall help and period controls stable while another period loads', async () => {
    await renderPage();

    const helpSlot = container.querySelector('[data-ranking-score-explanation-slot]');
    const periodControl = container.querySelector('[data-ranking-period]');
    expect(helpSlot).not.toBeNull();
    expect(periodControl).not.toBeNull();

    await renderPage({
      period: 'yesterday',
      leaderboard,
      leaderboardLoading: true,
    });
    expect(container.querySelector('[data-ranking-score-explanation-slot]')).toBe(helpSlot);
    expect(container.querySelector('[data-ranking-period]')).toBe(periodControl);

    await renderPage({
      period: 'yesterday',
      leaderboard: { ...leaderboard, period: 'yesterday', period_key: '2026-07-23' },
      leaderboardLoading: false,
    });
    expect(container.querySelector('[data-ranking-score-explanation-slot]')).toBe(helpSlot);
    expect(container.querySelector('[data-ranking-period]')).toBe(periodControl);

    await renderPage({
      period: 'yesterday',
      metric: 'total_tokens',
      leaderboard: null,
      leaderboardLoading: true,
    });
    expect(container.querySelector('[data-ranking-score-explanation-slot]')).toBeNull();
  });

  it('changes ranking metrics from the title-shaped select', async () => {
    const onMetricChange = vi.fn();
    await renderPage({ onMetricChange });

    const trigger = container.querySelector<HTMLButtonElement>('[data-ranking-metric-title] button');
    await act(async () => trigger!.click());
    const requestMetric = Array.from(document.querySelectorAll<HTMLButtonElement>('[role="option"]'))
      .find((option) => option.textContent?.includes('ranking.metric_request_count'));
    await act(async () => requestMetric!.click());

    expect(onMetricChange).toHaveBeenCalledWith('request_count');
  });

  it('keeps the leaderboard as the only page card and moves participation into one large modal', async () => {
    await renderPage();

    expect(container.querySelectorAll('article.card')).toHaveLength(1);
    expect(container.textContent).not.toContain('ranking.title');
    expect(container.textContent).not.toContain('ranking.privacy_description');
    expect(container.querySelector('[data-ranking-upload-field]')).toBeNull();
    expect(container.querySelector('[data-ranking-avatar-option]')).toBeNull();
    expect(container.querySelector('[data-ranking-profile-action]')?.textContent).toContain('ranking.join');

    await openProfileModal();

    const dialog = document.querySelector<HTMLElement>('[role="dialog"]');
    const privacyHint = dialog?.querySelector<HTMLButtonElement>('[data-ranking-privacy-hint]');
    const privacyDescriptionID = privacyHint?.getAttribute('aria-describedby');
    const privacyDescription = privacyDescriptionID ? document.getElementById(privacyDescriptionID) : null;
    const privacyTooltipID = privacyHint?.getAttribute('aria-controls');
    const privacyTooltip = privacyTooltipID ? document.getElementById(privacyTooltipID) : null;
    expect(privacyHint?.getAttribute('title')).toBeNull();
    expect(privacyDescription?.textContent).toBe('ranking.privacy_description');
    expect(privacyHint?.getAttribute('aria-controls')).toBe(privacyTooltip?.id);
    expect(privacyHint?.getAttribute('aria-expanded')).toBe('false');
    expect(privacyTooltip?.getAttribute('role')).toBe('tooltip');
    expect(privacyTooltip?.textContent).toBe('ranking.privacy_description');
    await act(async () => privacyHint!.focus());
    expect(privacyHint?.getAttribute('aria-expanded')).toBe('true');
    await act(async () => privacyHint!.blur());
    expect(privacyHint?.getAttribute('aria-expanded')).toBe('false');
    expect(dialog?.querySelector('[data-ranking-upload-field]')).toBeNull();
    expect(dialog?.querySelectorAll('[data-ranking-avatar-option]')).toHaveLength(66);
  });

  it('keeps an accessible profile action name when mobile styling leaves only the active avatar visible', async () => {
    await renderPage({ status: { status: 'active', display_name: 'Owner', avatar_id: 7 } });

    const profileAction = container.querySelector<HTMLButtonElement>('[data-ranking-profile-action]');
    expect(profileAction?.getAttribute('aria-label')).toBe('Owner · ranking.profile_action');
    expect(profileAction?.querySelector('[data-ranking-profile-name]')?.textContent).toBe('Owner');
  });

  it('keeps a successful leaderboard visible when metadata loading fails', async () => {
    const onRetryMetadata = vi.fn(async () => null);
    await renderPage({
      metadataError: new RankingApiError('ranking_center_unavailable', 503),
      onRetryMetadata,
    });

    expect(container.querySelectorAll('[data-ranking-podium-rank]')).toHaveLength(3);
    const warning = container.querySelector('[data-ranking-metadata-warning]');
    expect(warning).not.toBeNull();
    await click('button', warning!);
    expect(onRetryMetadata).toHaveBeenCalledOnce();
  });

  it('uses the shared modal footer for cancel and join actions', async () => {
    await renderPage();
    await openProfileModal();

    const dialog = document.querySelector<HTMLElement>('[role="dialog"]');
    const footer = dialog?.querySelector('.modal-footer');
    expect(footer?.textContent).toContain('common.cancel');
    expect(footer?.querySelector('[data-ranking-join]')).not.toBeNull();
  });

  it('normalizes the profile and requires immutable-profile confirmation before joining', async () => {
    const onJoin = vi.fn(async () => null);
    await renderPage({ onJoin });
    await openProfileModal();

    const input = document.querySelector<HTMLInputElement>('input[name="ranking-display-name"]');
    expect(input).not.toBeNull();
    await setInput(input!, '  Ｋeeper_中-1  ');
    await click('[data-ranking-avatar-option="7"]');
    await click('[data-ranking-join]');

    expect(onJoin).not.toHaveBeenCalled();
    expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(1);
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('ranking.join_confirm_title');
    await click('[data-ranking-confirm-join]');
    expect(onJoin).toHaveBeenCalledWith({ display_name: 'Keeper_中-1', avatar_id: 7 });
  });

  it('treats the avatar picker as one keyboard stop and supports arrow-key selection', async () => {
    await renderPage();
    await openProfileModal();

    const avatars = [...document.querySelectorAll<HTMLButtonElement>('[data-ranking-avatar-option]')];
    expect(avatars.filter((avatar) => avatar.tabIndex === 0)).toHaveLength(1);
    expect(avatars[0]?.getAttribute('aria-checked')).toBe('true');

    await act(async () => {
      avatars[0].focus();
      avatars[0].dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));
    });

    const updated = [...document.querySelectorAll<HTMLButtonElement>('[data-ranking-avatar-option]')];
    expect(updated[1]?.getAttribute('aria-checked')).toBe('true');
    expect(updated[1]?.tabIndex).toBe(0);
    expect(document.activeElement).toBe(updated[1]);
  });

  it('renders duplicate names and overall metrics without displaying participant identifiers', async () => {
    await renderPage({ status: { status: 'active', display_name: 'Owner', avatar_id: 7 } });

    const podium = container.querySelectorAll('[data-ranking-podium-rank]');
    const rows = container.querySelectorAll('[data-ranking-row]');
    expect(rows).toHaveLength(5);
    expect([...podium].map((card) => card.textContent).join(' ').match(/SameName/g)).toHaveLength(2);
    for (const entry of leaderboard.entries) {
      expect(container.textContent).not.toContain(entry.participant_id);
    }
    expect(rows[0]?.querySelector('[data-ranking-position]')?.textContent).toBe('1');
    expect(rows[4]?.querySelector('[data-ranking-position]')?.textContent).toBe('5');
  });

  it('highlights the first three entries in podium cards and keeps the complete ranking in the table', async () => {
    await renderPage();

    const podium = [...container.querySelectorAll('[data-ranking-podium-rank]')];
    const rows = container.querySelectorAll('[data-ranking-row]');
    expect(podium).toHaveLength(3);
    expect(podium.map((card) => card.getAttribute('data-ranking-podium-rank'))).toEqual(['1', '2', '3']);
    expect(podium[0]?.textContent).toContain('SameName');
    expect(podium[0]?.textContent).toContain('93.25 PTS');
    expect(podium[2]?.textContent).toContain('ThirdName');
    expect(rows).toHaveLength(5);
    expect(rows[0]?.textContent).toContain('SameName');
    expect(rows[0]?.querySelector('[data-ranking-position]')?.textContent).toBe('1');
    expect(rows[2]?.textContent).toContain('ThirdName');
    expect(rows[4]?.textContent).toContain('FifthName');
    expect(rows[4]?.querySelector('[data-ranking-position]')?.textContent).toBe('5');
  });

  it('renders podium scores as plain text without an interactive details tooltip', async () => {
    await renderPage();

    const firstPlace = container.querySelector('[data-ranking-podium-rank="1"]');
    const score = [...(firstPlace?.querySelectorAll('span') ?? [])]
      .find((element) => element.textContent === '93.25 PTS');
    expect(score).toBeDefined();
    expect(firstPlace?.querySelector('button')).toBeNull();
    expect(container.querySelector('[data-ranking-score-trigger]')).toBeNull();
    expect(document.body.querySelector('[data-ranking-score-tooltip]')).toBeNull();
  });

  it('keeps a current successful board visible when its refresh reports an error', async () => {
    await renderPage({
      leaderboardError: new RankingApiError('ranking_center_unavailable', 503),
    });

    expect(container.querySelectorAll('[data-ranking-row]')).toHaveLength(5);
    expect(container.querySelector('[role="alert"]')).toBeNull();
  });

  it('shows the retained period key when the visible board is stale', async () => {
    await renderPage({ leaderboard: { ...leaderboard, stale: true } });

    const staleBadge = container.querySelector('[data-ranking-stale]');
    expect(staleBadge).not.toBeNull();
    expect(staleBadge?.textContent).toContain('ranking.stale');
    expect(staleBadge?.textContent).toContain('2026-07-24');
  });

  it('marks profile and leaderboard avatars as decorative when their names are already visible', async () => {
    await renderPage({ status: { status: 'active', display_name: 'Owner', avatar_id: 7 } });
    const profileAction = container.querySelector('[data-ranking-profile-action]');
    expect(profileAction?.querySelector('[data-ranking-profile-name]')?.textContent).toBe('Owner');
    await openProfileModal();

    const visibleAvatars = document.querySelectorAll('[data-ranking-avatar-id]');
    expect(visibleAvatars.length).toBeGreaterThan(2);
    expect([...visibleAvatars].every((avatar) => avatar.getAttribute('aria-hidden') === 'true')).toBe(true);
    expect([...visibleAvatars].every((avatar) => !avatar.hasAttribute('role'))).toBe(true);
  });

  it('uses distinct fixed-column markers for the ranking and participant context', async () => {
    await renderPage();

    expect(container.querySelectorAll('[data-ranking-rank-column]')).toHaveLength(6);
    expect(container.querySelectorAll('[data-ranking-participant-column]')).toHaveLength(6);
  });

  it('makes the deleted tombstone permanent and removes every join action', async () => {
    await renderPage({ status: { status: 'deleted', display_name: 'Keeper_01', avatar_id: 7 } });
    expect(container.querySelector('[data-ranking-profile-action]')?.textContent).toContain('ranking.status_deleted');
    await openProfileModal();

    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('ranking.deleted_title');
    expect(document.querySelector('[data-ranking-join]')).toBeNull();
    expect(document.querySelector('input[name="ranking-display-name"]')).toBeNull();
  });

  it('shows a permanent ban distinctly from voluntary exit and uses the ban error message', async () => {
    const status = { status: 'deleted', banned: true, display_name: 'Keeper_01', avatar_id: 7 } as RankingStatusResponse;
    await renderPage({ status });
    expect(container.querySelector('[data-ranking-profile-action]')?.textContent).toBe('ranking.status_banned');
    await openProfileModal();
    const dialog = document.querySelector('[role="dialog"]');
    expect(dialog?.querySelector('.modal-title')?.textContent).toBe('ranking.banned_title');
    expect(dialog?.textContent).toContain('ranking.banned_description');
    expect(dialog?.textContent).not.toContain('ranking.deleted_description');
    expect(document.querySelector('[data-ranking-join]')).toBeNull();
    expect(document.querySelector('[data-ranking-resume]')).toBeNull();

    await renderPage({ status, actionError: new RankingApiError('ranking_participant_deleted', 410) });
    expect(document.querySelector('[data-ranking-action-feedback="error"]')?.textContent).toBe('ranking.error_banned');
  });

  it('requires a second confirmation before permanent exit', async () => {
    const onExit = vi.fn(async () => null);
    await renderPage({ status: { status: 'active', display_name: 'Keeper_01', avatar_id: 7 }, onExit });
    await openProfileModal();

    await click('[data-ranking-exit]');
    expect(onExit).not.toHaveBeenCalled();
    expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(1);
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('ranking.exit_confirm_title');
    await click('[data-ranking-confirm-exit]');
    expect(onExit).toHaveBeenCalledOnce();
  });

  it('uses a balanced four-action footer and confirms before pausing uploads', async () => {
    const onPause = vi.fn(async () => null);
    await renderPage({ status: { status: 'active', display_name: 'Keeper_01', avatar_id: 7 }, onPause });
    await openProfileModal();

    const footer = document.querySelector('[role="dialog"] .modal-footer');
    expect(footer?.querySelector('[data-ranking-exit]')).not.toBeNull();
    expect(footer?.querySelector('[data-ranking-sync]')).not.toBeNull();
    expect(footer?.querySelector('[data-ranking-pause]')).not.toBeNull();
    expect(footer?.querySelector('[data-ranking-close]')).not.toBeNull();
    expect(footer?.querySelectorAll('button')).toHaveLength(4);
    expect(document.querySelector('.modal-body [data-ranking-sync]')).toBeNull();

    await click('[data-ranking-pause]');
    expect(onPause).not.toHaveBeenCalled();
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('ranking.pause_confirm_body');
    await click('[data-ranking-confirm-pause]');
    expect(onPause).toHaveBeenCalledOnce();
  });

  it('keeps sync success and failure feedback inside the open profile modal', async () => {
    const syncedStatus = { status: 'active', display_name: 'Keeper_01', avatar_id: 7 } as RankingStatusResponse;
    const onSync = vi.fn(async () => syncedStatus);
    await renderPage({ status: syncedStatus, onSync });
    await openProfileModal();

    await click('[data-ranking-sync]');

    const dialog = document.querySelector('[role="dialog"]');
    const success = dialog?.querySelector('[data-ranking-action-feedback="success"]');
    expect(dialog).not.toBeNull();
    expect(success?.getAttribute('role')).toBe('status');
    expect(success?.textContent).toBe('ranking.action_sync_success');
    expect(dialog?.querySelector('[data-ranking-action-feedback="error"]')).toBeNull();

    await renderPage({
      status: syncedStatus,
      actionError: new RankingApiError('ranking_center_unavailable', 503),
      onSync,
    });

    const error = document.querySelector('[role="dialog"] [data-ranking-action-feedback="error"]');
    expect(error?.getAttribute('role')).toBe('alert');
    expect(document.querySelector('[role="dialog"] [data-ranking-action-feedback="success"]')).toBeNull();
  });

  it('shows specific in-modal success feedback after pause, resume, and exit', async () => {
    const activeStatus = { status: 'active', display_name: 'Keeper_01', avatar_id: 7 } as RankingStatusResponse;
    const pausedStatus = { status: 'paused', display_name: 'Keeper_01', avatar_id: 7 } as RankingStatusResponse;
    const deletedStatus = { status: 'deleted', display_name: 'Keeper_01', avatar_id: 7 } as RankingStatusResponse;
    const onPause = vi.fn(async () => pausedStatus);
    const onResume = vi.fn(async () => activeStatus);
    const onExit = vi.fn(async () => deletedStatus);
    await renderPage({ status: activeStatus, onPause, onResume, onExit });
    await openProfileModal();

    await click('[data-ranking-pause]');
    await click('[data-ranking-confirm-pause]');
    expect(document.querySelector('[role="dialog"] [data-ranking-action-feedback="success"]')?.textContent)
      .toBe('ranking.action_pause_success');

    await renderPage({ status: pausedStatus, onPause, onResume, onExit });
    await click('[data-ranking-resume]');
    expect(document.querySelector('[role="dialog"] [data-ranking-action-feedback="success"]')?.textContent)
      .toBe('ranking.action_resume_success');

    await renderPage({ status: activeStatus, onPause, onResume, onExit });
    await click('[data-ranking-exit]');
    await click('[data-ranking-confirm-exit]');
    expect(document.querySelector('[role="dialog"] [data-ranking-action-feedback="success"]')?.textContent)
      .toBe('ranking.action_exit_success');
  });

  it('keeps the registered avatar and name while paused and offers resume without syncing', async () => {
    const onResume = vi.fn(async () => null);
    await renderPage({ status: { status: 'paused', display_name: 'Keeper_01', avatar_id: 7 }, onResume });

    expect(container.querySelector('[data-ranking-profile-name]')?.textContent).toBe('Keeper_01');
    await openProfileModal();
    const dialog = document.querySelector('[role="dialog"]');
    expect(dialog?.textContent).toContain('ranking.paused_description');
    expect(dialog?.querySelector('[data-ranking-resume]')).not.toBeNull();
    expect(dialog?.querySelector('[data-ranking-sync]')).toBeNull();
    expect(dialog?.querySelector('[data-ranking-pause]')).toBeNull();

    await click('[data-ranking-resume]', dialog!);
    expect(onResume).toHaveBeenCalledOnce();
  });

  it('formats short and long Retry-After limits without calling every request a registration', async () => {
    await renderPage({
      status: { status: 'active', display_name: 'Keeper_01', avatar_id: 7 },
      actionError: new RankingApiError('ranking_center_rate_limited', 429, '1'),
    });
    await openProfileModal();
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('ranking.error_rate_limited_seconds:{"seconds":1}');

    await renderPage({
      status: { status: 'active', display_name: 'Keeper_01', avatar_id: 7 },
      actionError: new RankingApiError('ranking_center_rate_limited', 429, '61'),
    });
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('ranking.error_rate_limited_minutes:{"minutes":2}');
  });

  it('clears both retained action feedback states when the profile modal closes', async () => {
    const FeedbackHarness = () => {
      const [actionError, setActionError] = useState<unknown>(new RankingApiError('ranking_center_unavailable', 503));
      const props = {
        ...defaultProps,
        status: { status: 'active', display_name: 'Keeper_01', avatar_id: 7 } as RankingStatusResponse,
        actionError,
        onClearActionError: () => setActionError(null),
      };
      return <RankingPage {...props} />;
    };
    await act(async () => root.render(<FeedbackHarness />));
    await openProfileModal();
    expect(document.querySelector('[role="dialog"] [data-ranking-action-feedback="error"]')).not.toBeNull();

    await click('[data-ranking-close]');
    await openProfileModal();

    expect(document.querySelector('[role="dialog"] [data-ranking-action-feedback="error"]')).toBeNull();
    expect(document.querySelector('[role="dialog"] [data-ranking-action-feedback="success"]')).toBeNull();
  });
});
