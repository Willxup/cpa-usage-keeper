// @vitest-environment happy-dom

import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { RankingAvatar } from '../RankingAvatar';

globalThis.IS_REACT_ACT_ENVIRONMENT = true;

describe('RankingAvatar', () => {
  let container: HTMLDivElement;
  let root: ReturnType<typeof createRoot>;

  beforeEach(() => {
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
  });

  it.each([
    [1, '0% 0%'],
    [10, '100% 0%'],
    [11, '0% 11.11111111111111%'],
  ])('maps avatar %i to its row-major sprite position', (avatarID, position) => {
    act(() => root.render(<RankingAvatar avatarID={avatarID} name="Keeper" />));

    const avatar = container.querySelector<HTMLElement>('[data-ranking-avatar-id]');
    expect(avatar?.dataset.rankingAvatarId).toBe(String(avatarID));
    expect(avatar?.style.backgroundPosition).toBe(position);
    expect(avatar?.style.backgroundImage).toContain('avatar-catalog.webp');
    expect(container.querySelector('img')).toBeNull();
  });

  it('can be hidden from assistive technology when surrounding content supplies the name', () => {
    act(() => root.render(<RankingAvatar avatarID={7} name="Keeper" decorative />));

    const avatar = container.querySelector<HTMLElement>('[data-ranking-avatar-id]');
    expect(avatar?.getAttribute('aria-hidden')).toBe('true');
    expect(avatar?.hasAttribute('role')).toBe(false);
    expect(avatar?.hasAttribute('aria-label')).toBe(false);
  });
});
