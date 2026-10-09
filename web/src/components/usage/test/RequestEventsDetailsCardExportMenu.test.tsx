// @vitest-environment happy-dom

import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { RequestEventsTestCard } from './requestEventsFixtures';

describe('RequestEventsDetailsCard export menu', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
  });

  it('opens only after a click and keeps the existing format selection behavior', async () => {
    const onExport = vi.fn();
    await act(async () => {
      root.render(<RequestEventsTestCard events={[]} onExport={onExport} />);
    });

    const trigger = container.querySelector<HTMLButtonElement>('button[aria-haspopup="menu"]')!;
    const menuRoot = trigger.closest<HTMLDivElement>('div')!;
    expect(container.querySelector('[role="menu"]')).toBeNull();

    await act(async () => {
      menuRoot.dispatchEvent(new MouseEvent('mouseover', { bubbles: true }));
    });
    expect(container.querySelector('[role="menu"]')).toBeNull();

    await act(async () => trigger.click());
    expect(container.querySelector('[role="menu"]')).not.toBeNull();

    await act(async () => {
      menuRoot.dispatchEvent(new MouseEvent('mouseout', { bubbles: true, relatedTarget: document.body }));
    });
    expect(container.querySelector('[role="menu"]')).not.toBeNull();

    const jsonItem = Array.from(container.querySelectorAll<HTMLButtonElement>('[role="menuitem"]'))
      .find((item) => item.textContent?.includes('JSON'))!;
    await act(async () => jsonItem.click());

    expect(onExport).toHaveBeenCalledExactlyOnceWith('json');
    expect(container.querySelector('[role="menu"]')).toBeNull();
  });
});
