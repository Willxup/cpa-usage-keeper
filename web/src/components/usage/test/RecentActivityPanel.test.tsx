import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it, vi } from 'vitest';
import '@/i18n';
import { RecentActivityPanel } from '../RecentActivityPanel';

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
    expect(html).toContain('Request Health Timeline');
    expect(html).toContain('aria-pressed="true">7d</button>');
    expect(html).toContain('>24h</button>');
    expect(html).toContain('>30d</button>');
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
});
