import { readFileSync } from 'node:fs';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { Button } from '../Button';
import { MainActionButton } from '../MainActionButton';

const componentsStyles = readFileSync(new URL('../../../styles/components.scss', import.meta.url), 'utf8');

describe('Button', () => {
  it('exposes the shared action appearance without changing its semantic variant', () => {
    const primary = renderToStaticMarkup(<Button appearance="action">Save</Button>);
    const danger = renderToStaticMarkup(
      <Button appearance="action" variant="danger">Delete</Button>,
    );

    expect(primary).toContain('class="btn btn-primary btn-action"');
    expect(danger).toContain('class="btn btn-danger btn-action"');
  });
});

describe('MainActionButton', () => {
  it('forwards button state and custom classes', () => {
    const html = renderToStaticMarkup(
      <MainActionButton
        shellClassName="page-action-shell"
        className="page-action-trigger"
        loading
        data-page-action="refresh"
      >
        Refresh
      </MainActionButton>,
    );

    expect(html).toContain('class="main-action-button-shell page-action-shell"');
    expect(html).toContain('class="btn btn-primary btn-action main-action-button page-action-trigger"');
    expect(html).toContain('data-page-action="refresh"');
    expect(html).toContain('aria-busy="true"');
    expect(html).toContain('disabled=""');
    expect(html).toContain('class="loading-spinner"');
  });

  it('respects reduced motion for the page action', () => {
    expect(componentsStyles).toMatch(
      /@media \(prefers-reduced-motion: reduce\) \{[\s\S]*?\.btn\.btn-action\.main-action-button[\s\S]*?transition: none;[\s\S]*?transform: none;/,
    );
  });
});
