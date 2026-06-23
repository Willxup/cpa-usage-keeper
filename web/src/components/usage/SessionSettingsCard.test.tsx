import React from 'react';
import '@/i18n';
import i18n from '@/i18n';
import { describe, expect, it, vi } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { getSessionLogoutConfirmationKeys, SessionSettingsCard } from './SessionSettingsCard';
import type { AuthManagedSessionItem } from '@/lib/types';

const sessions: AuthManagedSessionItem[] = [
  {
    id: 'current-admin-hash',
    kind: 'admin',
    role: 'admin',
    current: true,
    loginAt: '2026/06/20 10:00:00',
    expiresAt: '2026/06/20 12:00:00',
  },
  {
    id: 'other-admin-hash',
    kind: 'admin',
    role: 'admin',
    loginAt: '2026/06/20 10:05:00',
    expiresAt: '2026/06/20 12:05:00',
  },
  {
    id: 'hashed-session-id',
    kind: 'api_key',
    role: 'api_key_viewer',
    apiKeyId: '42',
    label: 'Team Key',
    displayKey: 'sk-*********123456',
    loginAt: '2026/06/20 10:10:00',
    expiresAt: '2026/06/27 10:10:00',
  },
];

const renderCard = (props: Partial<React.ComponentProps<typeof SessionSettingsCard>> = {}) => renderToStaticMarkup(
  <SessionSettingsCard
    sessions={sessions}
    loading={false}
    revokingId={null}
    onLogout={() => undefined}
    {...props}
  />,
);

describe('SessionSettingsCard', () => {
  it('renders admin and API key sessions with shared row details and current marker', () => {
    const html = renderCard();

    expect(html).toContain('Session Management');
    expect(html).toContain('Admin Session');
    expect(html).toContain('Current');
    expect(html).toContain('2026/06/20 10:00:00');
    expect(html).toContain('2026/06/20 12:00:00');
    expect(html).toContain('2026/06/20 10:05:00');
    expect(html).toContain('2026/06/20 12:05:00');
    expect(html).toContain('Team Key');
    expect(html).toContain('2026/06/20 10:10:00');
    expect(html).toContain('2026/06/27 10:10:00');
    expect(html).not.toContain('All admin sessions will be signed out together.');
    expect(html).not.toContain('sk-*********123456');
    expect(html).not.toContain('current-admin-hash');
    expect(html).not.toContain('other-admin-hash');
    expect(html).not.toContain('hashed-session-id');
    expect(html).not.toContain('api_key_viewer');
    expect((html.match(/>Sign out</g) ?? []).length).toBe(2);
  });

  it('renders loading and empty states', () => {
    expect(renderCard({ sessions: [], loading: true })).toContain('Loading...');
    expect(renderCard({ sessions: [], loading: false })).toContain('No active sessions.');
  });

  it('uses per-session warning copy for both admin and API key confirmations', () => {
    const adminKeys = getSessionLogoutConfirmationKeys(sessions[0]);
    const apiKeyKeys = getSessionLogoutConfirmationKeys(sessions[2]);

    expect(i18n.t(adminKeys.bodyKey)).toContain('this admin session');
    expect(i18n.t(adminKeys.bodyKey)).toContain('Other admin sessions');
    expect(i18n.t(adminKeys.bodyKey)).not.toContain('current device');
    expect(i18n.t(apiKeyKeys.bodyKey, { label: sessions[2].label })).toContain('Team Key');
    expect(i18n.t(apiKeyKeys.bodyKey, { label: sessions[2].label })).toContain('Other sessions');
    expect(adminKeys.bodyKey).not.toBe(apiKeyKeys.bodyKey);
  });

  it('disables the row currently being revoked', () => {
    const html = renderCard({ revokingId: 'hashed-session-id' });

    expect(html).toContain('Signing out');
    expect(html).toContain('disabled=""');
  });

  it('does not invoke logout while only rendering', () => {
    const onLogout = vi.fn();

    renderCard({ onLogout });

    expect(onLogout).not.toHaveBeenCalled();
  });
});
