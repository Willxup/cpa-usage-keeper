import { createElement, type ComponentProps } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it, vi } from 'vitest';
import '@/i18n';
import { RecentActivityPanel } from '../RecentActivityPanel';
import { buildUsageActivityFixture } from './activityFixtures';

const renderPanel = (props: Partial<ComponentProps<typeof RecentActivityPanel>> = {}) =>
  renderToStaticMarkup(createElement(RecentActivityPanel, {
    activity: null,
    loading: false,
    error: '',
    window: 'day',
    windowIsCurrent: true,
    requestIdentity: 'admin::day:::',
    onWindowChange: vi.fn(),
    ...props,
  }));

describe('RecentActivityPanel', () => {
  it('renders the section title and fixed window switcher above Request Health', () => {
    const html = renderPanel({
      window: 'week',
      requestIdentity: 'admin::2d:::',
    });

    expect(html).toContain('Recent Activity');
    expect(html).toContain('Token Activity');
    expect(html).toContain('Request Health Timeline');
    expect(html).toContain('aria-pressed="true">Week</button>');
  });

  it('keeps an Activity error inside the Recent Activity section', () => {
    const html = renderPanel({
      error: 'ACTIVITY_LOAD_FAILED',
      requestIdentity: 'admin::8h:::',
    });

    expect(html).toContain('Unable to load recent activity.');
    expect(html).not.toContain('ACTIVITY_LOAD_FAILED');
    expect(html).toContain('Recent Activity');
    expect(html).toContain('role="alert"');
  });

  it('marks only the Activity content as busy while refreshing', () => {
    const html = renderPanel({
      loading: true,
      window: null,
      windowIsCurrent: false,
      requestIdentity: 'admin::8h:::',
    });

    expect(html).toContain('aria-busy="true"');
  });

  it('shows the shared backend window once and gives both cards the same summary structure', () => {
    const html = renderPanel({
      activity: buildUsageActivityFixture([1_234]),
    });
    const sharedWindow = '07/01 00:00 – 07/02 00:00';

    expect(html.match(new RegExp(sharedWindow, 'g'))).toHaveLength(1);
    expect(html.indexOf(sharedWindow)).toBeLessThan(html.indexOf('>Day</button>'));
    expect(html.match(/data-activity-summary=/g)).toHaveLength(2);
    expect(html).toContain('data-activity-summary="token"');
    expect(html).toContain('data-activity-summary="health"');
    expect(html).toContain('Total tokens');
    expect(html).toContain('Success Rate');
  });
});
