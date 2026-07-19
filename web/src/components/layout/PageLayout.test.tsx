import { readFileSync } from 'node:fs';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { PageContent, PageHeader, PageTitle, PageToolbar } from './PageLayout';

const styles = readFileSync(new URL('./PageLayout.module.scss', import.meta.url), 'utf8');
const themes = readFileSync(new URL('../../styles/themes.scss', import.meta.url), 'utf8');

describe('PageLayout primitives', () => {
  it('renders one semantic page heading and explicit header stickiness', () => {
    const html = renderToStaticMarkup(
      <>
        <PageHeader sticky="always">
          <PageTitle>Usage</PageTitle>
        </PageHeader>
        <PageHeader sticky="desktop">Viewer</PageHeader>
      </>,
    );

    expect(html.match(/<h1/g)).toHaveLength(1);
    expect(html).toContain('data-sticky="always"');
    expect(html).toContain('data-sticky="desktop"');
  });

  it('provides shared content and toolbar regions without owning feature behavior', () => {
    const html = renderToStaticMarkup(
      <PageContent>
        <PageToolbar leading={<span>Context</span>} actions={<button type="button">Refresh</button>} />
      </PageContent>,
    );

    expect(html).toContain('Context');
    expect(html).toContain('Refresh');
    expect(html).not.toContain('api');
  });

  it('owns the shared max-width, gutters, stack, and sticky modes', () => {
    expect(styles).toContain('width: min(var(--page-max-width), 100%);');
    expect(styles).toContain('padding: 12px var(--page-gutter) !important;');
    expect(styles).toContain('gap: var(--page-stack);');
    expect(styles).toMatch(/\.stickyAlways,[\s\S]*?\.stickyDesktop\s*\{[\s\S]*?position:\s*sticky;/);
    expect(styles).toMatch(/@include mobile\s*\{[\s\S]*?\.stickyDesktop\s*\{[\s\S]*?position:\s*static;/);
    expect(themes).toContain('--page-max-width: 1440px;');
    expect(themes).toContain('--page-gutter: 24px;');
    expect(themes).toContain('--page-gutter-mobile: 12px;');
    expect(themes).toContain('--page-stack: 20px;');
  });
});
