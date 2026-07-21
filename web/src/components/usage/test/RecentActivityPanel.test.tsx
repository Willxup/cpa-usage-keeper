import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it, vi } from 'vitest';
import '@/i18n';
import { RecentActivityPanel } from '../RecentActivityPanel';
import { buildUsageActivityFixture } from './activityFixtures';

describe('RecentActivityPanel', () => {
  it('renders the section title and fixed window switcher above Request Health', () => {
    const html = renderToStaticMarkup(createElement(RecentActivityPanel, {
      activity: null,
      loading: false,
      error: '',
      window: '7d',
      requestIdentity: 'admin::2d:::',
      onWindowChange: vi.fn(),
    }));

    expect(html).toContain('Recent Activity');
    expect(html).toContain('Token Activity');
    expect(html).toContain('Request Health Timeline');
    expect(html).toContain('aria-pressed="true">7d</button>');
    expect(html).toContain('>24h</button>');
    expect(html).toContain('>30d</button>');
    expect(html).toContain('>1y</button>');
  });

  it('keeps an Activity error inside the Recent Activity section', () => {
    const html = renderToStaticMarkup(createElement(RecentActivityPanel, {
      activity: null,
      loading: false,
      error: 'ACTIVITY_LOAD_FAILED',
      window: '24h',
      requestIdentity: 'admin::8h:::',
      onWindowChange: vi.fn(),
    }));

    expect(html).toContain('Unable to load recent activity.');
    expect(html).not.toContain('ACTIVITY_LOAD_FAILED');
    expect(html).toContain('Recent Activity');
    expect(html).toContain('role="alert"');
  });

  it('marks only the Activity content as busy while refreshing', () => {
    const html = renderToStaticMarkup(createElement(RecentActivityPanel, {
      activity: null,
      loading: true,
      error: '',
      window: null,
      requestIdentity: 'admin::8h:::',
      onWindowChange: vi.fn(),
    }));

    expect(html).toContain('aria-busy="true"');
  });

  it('shows the shared backend window once and gives both cards the same summary structure', () => {
    const html = renderToStaticMarkup(createElement(RecentActivityPanel, {
      activity: buildUsageActivityFixture([1_234]),
      loading: false,
      error: '',
      window: '24h',
      requestIdentity: 'admin::24h:::',
      onWindowChange: vi.fn(),
    }));
    const sharedWindow = '07/01 00:00 – 07/02 00:00';

    expect(html.match(new RegExp(sharedWindow, 'g'))).toHaveLength(1);
    expect(html.indexOf(sharedWindow)).toBeLessThan(html.indexOf('>24h</button>'));
    expect(html.match(/data-activity-summary=/g)).toHaveLength(2);
    expect(html).toContain('data-activity-summary="token"');
    expect(html).toContain('data-activity-summary="health"');
    expect(html).toContain('Total tokens');
    expect(html).toContain('Success Rate');
  });
});
