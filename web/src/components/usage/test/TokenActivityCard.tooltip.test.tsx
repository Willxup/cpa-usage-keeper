// @vitest-environment happy-dom

import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import i18n from '@/i18n';
import { TokenActivityCard } from '../TokenActivityCard';
import { buildUsageActivityFixture } from './activityFixtures';

describe('TokenActivityCard tooltip', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(async () => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
    await i18n.changeLanguage('en');
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    document.body.replaceChildren();
  });

  it('compacts visible token values while keeping exact values in the cell label', async () => {
    const activity = buildUsageActivityFixture();
    activity.blocks[0] = {
      ...activity.blocks[0],
      total_tokens: 74_174_604,
      input_tokens: 73_893_802,
      output_tokens: 280_802,
      reasoning_tokens: 160_430,
      cache_read_tokens: 69_897_984,
      cache_creation_tokens: 0,
    };

    await act(async () => root.render(
      <TokenActivityCard activity={activity} loading={false} requestIdentity="admin::24h:::" />,
    ));
    const firstCell = container.querySelector<HTMLElement>('[role="gridcell"]');
    expect(firstCell).not.toBeNull();

    await act(async () => firstCell?.focus());

    const tooltipText = document.body.querySelector('[role="tooltip"]')?.textContent ?? '';
    expect(tooltipText).toContain('Total 74.17M');
    expect(tooltipText).toContain('Input 73.89M');
    expect(tooltipText).toContain('Output 280.80K');
    expect(tooltipText).toContain('Reasoning 160.43K');
    expect(tooltipText).toContain('Cache Read 69.90M');
    expect(tooltipText).toContain('Cache Creation 0');
    expect(firstCell?.getAttribute('aria-label')).toContain('Total 74,174,604');
    expect(firstCell?.getAttribute('aria-label')).toContain('Cache Read 69,897,984');
  });
});
