import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it, vi } from 'vitest';
import { CLIPROXYAPI_REPOSITORY_URL, GITHUB_PROFILE_URL, GITHUB_REPOSITORY_URL } from '@/utils/constants';
import { aboutVersionLabel, loadAboutVersion, ProductAboutContent } from './ProductAbout';

describe('ProductAbout', () => {
  it('renders project links, powered by line, and version label', () => {
    const html = renderToStaticMarkup(<ProductAboutContent version="v1.2.3" />);

    expect(html).toContain('© 2026');
    expect(html).toContain(`href="${GITHUB_REPOSITORY_URL}"`);
    expect(html).toContain('<strong>CPA Usage Keeper</strong>');
    expect(html).toContain('License');
    expect(html).toContain('MIT License');
    expect(html).toContain('CLIProxyAPI Integration');
    expect(html).toContain(`href="${GITHUB_REPOSITORY_URL}/blob/main/LICENSE"`);
    expect(html).toContain(`href="${CLIPROXYAPI_REPOSITORY_URL}"`);
    expect(html).toContain('Powered By');
    expect(html).toContain('aria-label="Willxup GitHub profile"');
    expect(html).toContain('<svg');
    expect(html).toContain('Willxup');
    expect(html).toContain('Version: v1.2.3');
    expect(html).toContain('Project resources');
  });

  it('does not render a version label before the version is available', () => {
    const html = renderToStaticMarkup(<ProductAboutContent />);

    expect(html).not.toContain('Version:');
  });

  it('respects disabled version loading while still allowing a fixed version', () => {
    const unloadedHtml = renderToStaticMarkup(<ProductAboutContent loadVersion={false} />);
    const fixedHtml = renderToStaticMarkup(<ProductAboutContent version="v1.2.3" loadVersion={false} />);

    expect(unloadedHtml).not.toContain('Version:');
    expect(fixedHtml).toContain('Version: v1.2.3');
  });

  it('formats only non-empty version values', () => {
    expect(aboutVersionLabel('v1.2.3')).toBe('Version: v1.2.3');
    expect(aboutVersionLabel('dev')).toBe('Version: dev');
    expect(aboutVersionLabel('')).toBeUndefined();
    expect(aboutVersionLabel(undefined)).toBeUndefined();
  });

  it('loads the about version through the provided version loader', async () => {
    const signal = new AbortController().signal;
    const loadVersion = vi.fn(async () => ({ version: 'v1.2.3', updateCheckEnabled: true }));

    await expect(loadAboutVersion(loadVersion, signal)).resolves.toBe('v1.2.3');

    expect(loadVersion).toHaveBeenCalledWith(signal);
  });

  it('falls back to an empty version when version loading fails', async () => {
    const signal = new AbortController().signal;
    const loadVersion = vi.fn(async () => {
      throw new Error('network failed');
    });

    await expect(loadAboutVersion(loadVersion, signal)).resolves.toBe('');
  });
});
