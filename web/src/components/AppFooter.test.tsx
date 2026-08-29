import { readFileSync } from 'node:fs';
import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it, vi } from 'vitest';
import { CLIPROXYAPI_REPOSITORY_URL, GITHUB_PROFILE_URL, GITHUB_REPOSITORY_URL } from '@/utils/constants';
import { AppFooter, footerVersionLabel, loadFooterVersion } from './AppFooter';

const appStyles = readFileSync(new URL('../App.css', import.meta.url), 'utf8').replace(/\r\n/g, '\n');

describe('AppFooter', () => {
  it('renders project links, powered by line, and version label', () => {
    const html = renderToStaticMarkup(<AppFooter version="v1.2.3" />);

    expect(html).toContain('© 2026');
    expect(html).toContain(`href="${GITHUB_REPOSITORY_URL}"`);
    expect(html).toContain('>CPA Usage Keeper</a>');
    expect(html).toContain('License');
    expect(html).toContain('CLIProxyAPI Integration');
    expect(html).toContain('class="app-footer-line app-footer-meta"');
    expect(html).toContain('class="app-footer-line app-footer-powered"');
    expect(html).toContain('Powered By');
    expect(html).toContain('aria-label="Willxup GitHub profile"');
    expect(html).toContain('<svg');
    expect(html).toContain('Willxup');
    expect(html).toContain('Version: v1.2.3');
    expect(html).toContain(`CPA Usage Keeper</a><span>·</span><a href="${GITHUB_REPOSITORY_URL}/blob/main/LICENSE"`);
    expect(html).toContain(`License</a><span>·</span><a href="${CLIPROXYAPI_REPOSITORY_URL}"`);
    expect(html).toContain(`href="${GITHUB_PROFILE_URL}"`);
    expect(html).toContain('Willxup</span></a><span class="app-footer-version-separator" aria-hidden="true">·</span><span class="app-footer-version">Version: v1.2.3</span>');
    expect(html).not.toContain('|');
  });

  it('does not render a version label before the version is available', () => {
    const html = renderToStaticMarkup(<AppFooter />);

    expect(html).not.toContain('Version:');
  });

  it('respects disabled version loading while still allowing a fixed version', () => {
    const unloadedHtml = renderToStaticMarkup(<AppFooter loadVersion={false} />);
    const fixedHtml = renderToStaticMarkup(<AppFooter version="v1.2.3" loadVersion={false} />);

    expect(unloadedHtml).not.toContain('Version:');
    expect(fixedHtml).toContain('Version: v1.2.3');
  });

  it('formats only non-empty version values', () => {
    expect(footerVersionLabel('v1.2.3')).toBe('Version: v1.2.3');
    expect(footerVersionLabel('dev')).toBe('Version: dev');
    expect(footerVersionLabel('')).toBeUndefined();
    expect(footerVersionLabel(undefined)).toBeUndefined();
  });

  it('loads footer version through the provided version loader', async () => {
    const signal = new AbortController().signal;
    const loadVersion = vi.fn(async () => ({ version: 'v1.2.3', updateCheckEnabled: true }));

    await expect(loadFooterVersion(loadVersion, signal)).resolves.toBe('v1.2.3');

    expect(loadVersion).toHaveBeenCalledWith(signal);
  });

  it('falls back to an empty footer version when version loading fails', async () => {
    const signal = new AbortController().signal;
    const loadVersion = vi.fn(async () => {
      throw new Error('network failed');
    });

    await expect(loadFooterVersion(loadVersion, signal)).resolves.toBe('');
  });

  it('keeps the version label visible on its own mobile footer line', () => {
    const mobileFooterStyles = appStyles.slice(appStyles.indexOf('@media (max-width: 640px)'));
    expect(mobileFooterStyles).toContain('.app-footer-version-separator');
    expect(mobileFooterStyles).not.toContain(':has(');
    expect(mobileFooterStyles).toMatch(/\.app-footer-version-separator\s*\{[\s\S]*?display:\s*none;/);
    expect(mobileFooterStyles).toMatch(/\.app-footer-version\s*\{[\s\S]*?display:\s*block;[\s\S]*?flex-basis:\s*100%;/);
  });
});
