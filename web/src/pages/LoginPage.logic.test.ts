import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { getLoginErrorForMode } from './LoginPage';

const source = readFileSync(new URL('./LoginPage.tsx', import.meta.url), 'utf8');
const stylesSource = readFileSync(new URL('./LoginPage.module.scss', import.meta.url), 'utf8');
const shellSource = readFileSync(new URL('../components/layout/AppShell.tsx', import.meta.url), 'utf8');
const shellStyles = readFileSync(new URL('../components/layout/AppShell.module.scss', import.meta.url), 'utf8');

describe('LoginPage mode-specific errors', () => {
  it('shows only the active login mode error', () => {
    expect(getLoginErrorForMode('admin', { adminError: 'bad password', apiKeyError: 'bad api key' })).toBe('bad password');
    expect(getLoginErrorForMode('api_key', { adminError: 'bad password', apiKeyError: 'bad api key' })).toBe('bad api key');
  });

  it('does not leak API Key failures onto the admin tab or admin failures onto the API Key tab', () => {
    expect(getLoginErrorForMode('admin', { adminError: '', apiKeyError: 'bad api key' })).toBe('');
    expect(getLoginErrorForMode('api_key', { adminError: 'bad password', apiKeyError: '' })).toBe('');
  });

  it('keeps the login hero concise and exposes compact preferences', () => {
    expect(source).toContain('<PreferencesDropdown />');
    expect(source).not.toContain('<Segmented');
    expect(source).not.toContain('capabilityGrid');
    expect(source).not.toContain('capability_persistence');
  });
});

describe('LoginPage guest shell', () => {
  it('delegates the guest chrome to the shared AppShell guest variant', () => {
    expect(source).toContain('<AppShell');
    expect(source).toContain("variant={embedded ? 'embed' : 'guest'}");
    expect(source).toContain('sticky="static"');
    expect(source).toContain("nav={{ mode: 'none' }}");
    expect(source).not.toContain('<Layout');
    expect(source).not.toContain('<Sider');
    expect(source).not.toContain('<Menu');
    expect(source).not.toContain('<Drawer');
    expect(source).not.toContain('<PageHeader');
    expect(source).not.toContain('<PageContent');
    expect(source).not.toContain('data-keeper-page');
    expect(source).not.toContain('footer:');
  });

  it('anchors the brand and preferences in the shared full-width header', () => {
    expect(source).toContain('brand: <BrandLink className={styles.brandLink} />');
    expect(source).toContain("headerTitle: t('auth.login_title')");
    expect(source).toContain('headerUtility: <PreferencesDropdown />');
    expect(shellSource).toContain("guest: 'none'");
    expect(shellStyles).toContain('width: min(var(--page-max-width), 100%);');
    expect(shellStyles).toContain('padding: 12px var(--page-gutter);');
  });

  it('keeps the header utility outside the bounded login column', () => {
    expect(source).not.toContain('utilityDock');
    expect(source).not.toContain('brandBlock');
    expect(stylesSource).not.toMatch(/\.utilityDock\s*\{/);
    expect(stylesSource).not.toMatch(/\.brandBlock\s*\{/);
    expect(stylesSource).not.toMatch(/\.title\s*\{/);
    expect(stylesSource).toMatch(/\.loginColumn\s*\{[\s\S]*?justify-content:\s*center;/);
    expect(stylesSource).toMatch(/\.loginCard\s*\{[\s\S]*?width:\s*min\(440px, 100%\);/);
  });

  it('fills the app main area through the shell without a second viewport height', () => {
    expect(shellStyles).toMatch(/\.appShellMain\s*\{[\s\S]*?flex:\s*1\s+1\s+auto;/);
    expect(shellStyles).toMatch(/\.appShellMain\s*\{[\s\S]*?min-height:\s*100svh;/);
    expect(stylesSource).not.toMatch(/\.pageShell\s*\{/);
    expect(stylesSource).not.toMatch(/\.frame\s*\{/);
    expect(stylesSource).not.toContain('min-height: 100vh');
  });

  it('keeps the login card full width on mobile', () => {
    expect(stylesSource).toMatch(/@include mobile\s*\{[\s\S]*?\.loginCard\s*\{[\s\S]*?width:\s*100%;/);
  });
});
