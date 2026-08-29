// @vitest-environment happy-dom

import { act, createElement } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, describe, expect, it } from 'vitest';
import '@/i18n';
import { ServiceHealthCard } from '../ServiceHealthCard';
import { buildUsageActivityFixture } from './activityFixtures';

describe('ServiceHealthCard tooltip emphasis', () => {
  afterEach(() => {
    document.body.replaceChildren();
  });

  it('bolds complete OK and Fail statuses while keeping the rate secondary', () => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
    const container = document.createElement('div');
    document.body.appendChild(container);
    const root = createRoot(container);

    act(() => root.render(createElement(ServiceHealthCard, {
      activity: buildUsageActivityFixture(),
      loading: false,
      requestIdentity: 'admin::day:::',
    })));
    const cells = container.querySelectorAll<HTMLElement>('[role="gridcell"]');
    act(() => cells.item(cells.length - 1).focus());

    const tooltip = document.querySelector<HTMLElement>('[role="tooltip"]');
    const emphasizedStatuses = Array.from(tooltip?.querySelectorAll('strong') ?? [], (element) => element.textContent);
    expect(emphasizedStatuses).toEqual(['OK 2', 'Fail 1']);
    expect(tooltip?.textContent).toContain('(66.7%)');

    act(() => root.unmount());
  });
});
