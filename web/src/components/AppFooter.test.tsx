import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { CLIPROXYAPI_REPOSITORY_URL, GITHUB_PROFILE_URL, GITHUB_REPOSITORY_URL } from '@/utils/constants';
import { AppFooter, footerVersionLabel } from './AppFooter';

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
    expect(html).toContain('Willxup</span></a><span>·</span><span class="app-footer-version">Version: v1.2.3</span>');
    expect(html).not.toContain('|');
    expect(html).not.toContain('app-footer-separator');
  });

  it('does not render a version label before the version is available', () => {
    const html = renderToStaticMarkup(<AppFooter />);

    expect(html).not.toContain('Version:');
  });

  it('formats only non-empty version values', () => {
    expect(footerVersionLabel('v1.2.3')).toBe('Version: v1.2.3');
    expect(footerVersionLabel('dev')).toBe('Version: dev');
    expect(footerVersionLabel('')).toBeUndefined();
    expect(footerVersionLabel(undefined)).toBeUndefined();
  });
});
