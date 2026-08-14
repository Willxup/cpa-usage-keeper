import React from 'react';
import '@/i18n';
import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { SessionSettingsCard } from '../SessionSettingsCard';
import type { AuthManagedSessionItem } from '@/lib/types';

describe('SessionSettingsCard client metadata', () => {
  it('renders the raw User-Agent, login IP, changed recent IP, and recent activity', () => {
    const userAgent = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 Version/18.0 Safari/605.1.15';
    const session: AuthManagedSessionItem = {
      id: 'session-hash',
      kind: 'admin',
      role: 'admin',
      source: 'standard',
      userAgent,
      loginIp: '203.0.113.9',
      lastSeenIp: '198.51.100.42',
      loginAt: '2026/08/13 10:00:00',
      lastSeenAt: '2026/08/13 10:15:00',
      expiresAt: '2026/08/20 10:00:00',
    };

    const html = renderToStaticMarkup(
      <SessionSettingsCard sessions={[session]} onLogout={() => undefined} />,
    );

    expect(html).toContain(userAgent);
    expect(html.split(userAgent)).toHaveLength(2);
    expect(html).toContain('User-Agent');
    expect(html).toMatch(/<dt[^>]*>Login IP<\/dt><dd[^>]*>203\.0\.113\.9<\/dd>/);
    expect(html).toMatch(/<dt[^>]*>Recent IP<\/dt><dd[^>]*>198\.51\.100\.42<\/dd>/);
    expect(html).toMatch(/<dt[^>]*>Last active<\/dt><dd[^>]*>2026\/08\/13 10:15:00<\/dd>/);
  });

  it('renders an honest fallback for sessions created before metadata collection', () => {
    const legacySession: AuthManagedSessionItem = {
      id: 'legacy-session-hash',
      kind: 'admin',
      role: 'admin',
      source: 'standard',
      loginAt: '2026/08/01 10:00:00',
      lastSeenAt: '2026/08/01 10:00:00',
      expiresAt: '2026/08/08 10:00:00',
    };

    const html = renderToStaticMarkup(
      <SessionSettingsCard sessions={[legacySession]} onLogout={() => undefined} />,
    );

    expect(html).toContain('Unknown');
    expect(html).toMatch(/<dt[^>]*>Login IP<\/dt><dd[^>]*>Unknown<\/dd>/);
  });
});
