// @vitest-environment happy-dom

import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { RankingLeaderboardResults } from '../RankingLeaderboardResults';
import type { RankingLeaderboardEntry } from '../../types';

globalThis.IS_REACT_ACT_ENVIRONMENT = true;

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, params?: Record<string, string | number>) => params ? `${key}:${JSON.stringify(params)}` : key,
  }),
}));

const entries: RankingLeaderboardEntry[] = [
  { rank: 1, participant_id: '1', display_name: 'Alpha', avatar_id: 1, value: 100 },
  { rank: 2, participant_id: '2', display_name: 'Beta', avatar_id: 2, value: 90 },
  { rank: 3, participant_id: '3', display_name: 'Gamma', avatar_id: 3, value: 80 },
  { rank: 4, participant_id: '4', display_name: 'Delta', avatar_id: 4, value: 70 },
];

describe('RankingLeaderboardResults', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    document.body.replaceChildren();
  });

  it('renders the same local podium and table as a read-only view without an edit callback', async () => {
    await act(async () => {
      root.render(<RankingLeaderboardResults scope="local" metric="overall" entries={entries} />);
    });

    expect(container.querySelectorAll('[data-ranking-podium-rank]')).toHaveLength(3);
    expect(container.querySelectorAll('[data-ranking-row]')).toHaveLength(4);
    expect(container.querySelectorAll('[data-ranking-local-profile-edit]')).toHaveLength(0);
    expect(container.querySelector('[data-ranking-participant-column]')?.textContent).toBe('ranking.api_key');
  });

  it('exposes both podium and table avatar edits only when the caller provides the action', async () => {
    const onEditLocalProfile = vi.fn();
    await act(async () => {
      root.render(
        <RankingLeaderboardResults
          scope="local"
          metric="overall"
          entries={entries}
          onEditLocalProfile={onEditLocalProfile}
        />,
      );
    });

    await act(async () => {
      container.querySelector<HTMLButtonElement>(
        '[data-ranking-podium-rank="1"] [data-ranking-local-profile-edit="1"]',
      )?.click();
    });
    await act(async () => {
      container.querySelector<HTMLButtonElement>(
        'tbody [data-ranking-local-profile-edit="4"]',
      )?.click();
    });

    expect(onEditLocalProfile).toHaveBeenNthCalledWith(1, entries[0]);
    expect(onEditLocalProfile).toHaveBeenNthCalledWith(2, entries[3]);
  });
});
