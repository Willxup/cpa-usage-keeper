// @vitest-environment happy-dom

import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { RankingScopeSwitch } from '../RankingScopeSwitch';

globalThis.IS_REACT_ACT_ENVIRONMENT = true;

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

describe('RankingScopeSwitch', () => {
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

  it('uses a two-state pressed-button control instead of a dropdown', () => {
    const onChange = vi.fn();
    act(() => root.render(<RankingScopeSwitch value="community" onChange={onChange} />));

    const switcher = container.querySelector('[data-ranking-scope-switch]');
    const local = container.querySelector<HTMLButtonElement>('[data-ranking-scope-option="local"]');
    const community = container.querySelector<HTMLButtonElement>('[data-ranking-scope-option="community"]');
    expect(switcher?.getAttribute('role')).toBe('group');
    expect(switcher?.querySelector('select')).toBeNull();
    expect(switcher?.textContent).toBe('ranking.scope_localranking.scope_community');
    expect(local?.getAttribute('aria-pressed')).toBe('false');
    expect(community?.getAttribute('aria-pressed')).toBe('true');

    act(() => local?.click());
    expect(onChange).toHaveBeenCalledWith('local');
  });

});
