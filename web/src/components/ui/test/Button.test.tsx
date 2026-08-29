import { resolve } from 'node:path';
import React, { type ComponentType } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { compile } from 'sass';
import { describe, expect, it } from 'vitest';
import { Button } from '../Button';
import { MainActionButton } from '../MainActionButton';

const componentsCSS = compile(resolve(process.cwd(), 'src/styles/components.scss')).css;

type ActionButtonProbeProps = React.ComponentProps<typeof Button> & {
  appearance?: 'action';
};

const ActionButtonProbe = Button as ComponentType<ActionButtonProbeProps>;

describe('Button', () => {
  it('exposes the shared action appearance without changing its semantic variant', () => {
    const primary = renderToStaticMarkup(<ActionButtonProbe appearance="action">Save</ActionButtonProbe>);
    const danger = renderToStaticMarkup(
      <ActionButtonProbe appearance="action" variant="danger">Delete</ActionButtonProbe>,
    );

    expect(primary).toContain('class="btn btn-primary btn-action"');
    expect(danger).toContain('class="btn btn-danger btn-action"');
  });

  it('keeps action buttons on the established compact pill contract', () => {
    expect(componentsCSS).toMatch(
      /\.btn-action \{[^}]*min-height: 32px;[^}]*border-radius: 999px;[^}]*padding: 7px 12px;[^}]*font-size: 12px;/,
    );
    expect(componentsCSS).toMatch(
      /\.btn-action\.btn-secondary, \.btn-action\.btn-ghost \{\s*box-shadow: 0 8px 20px rgba\(0, 0, 0, 0\.08\);\s*\}/,
    );
    expect(componentsCSS).toMatch(
      /\.btn-action\.btn-danger \{\s*box-shadow: none;\s*\}/,
    );
    expect(componentsCSS).toMatch(/\.btn\.btn-secondary \{[^}]*background-color: var\(--bg-tertiary\);/);
    expect(componentsCSS).toMatch(/\.btn\.btn-danger \{[^}]*background-color: var\(--danger-color\);/);
  });
});

describe('MainActionButton', () => {
  it('provides a dedicated page-level action primitive', async () => {
    const modulePath = '../MainActionButton';

    await expect(import(modulePath)).resolves.toMatchObject({
      MainActionButton: expect.any(Function),
    });
  });

  it('renders the 42px shell and 32px action trigger while forwarding button state', () => {
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

  it('matches the joined profile interaction with a theme surface hover animation', () => {
    expect(componentsCSS).toMatch(
      /\.main-action-button-shell \{[^}]*min-height: 42px;[^}]*padding: 4px;[^}]*border-radius: 999px;/,
    );
    expect(componentsCSS).toMatch(
      /\.btn\.btn-action\.main-action-button \{[^}]*min-height: 32px;[^}]*min-width: 0;[^}]*max-width: 100%;[^}]*border: 0;[^}]*background: var\(--bg-primary\);[^}]*color: var\(--text-primary\);/,
    );
    expect(componentsCSS).toMatch(
      /\.btn\.btn-action\.main-action-button > span:not\(\.loading-spinner\) \{[^}]*min-width: 0;[^}]*max-width: 100%;/,
    );
    expect(componentsCSS).toMatch(
      /\.btn\.btn-action\.main-action-button:hover:not\(:disabled\) \{[^}]*background: var\(--bg-primary\);[^}]*color: var\(--text-primary\);[^}]*box-shadow: 0 8px 20px rgba\(0, 0, 0, 0\.1\);[^}]*transform: translateY\(-1px\);/,
    );
    expect(componentsCSS).toMatch(
      /\.btn\.btn-action\.main-action-button:active:not\(:disabled\) \{[^}]*background: var\(--bg-primary\);[^}]*color: var\(--text-primary\);[^}]*transform: translateY\(0\);/,
    );
    expect(componentsCSS).toMatch(
      /@media \(prefers-reduced-motion: reduce\) \{[\s\S]*?\.btn\.btn-action\.main-action-button[\s\S]*?transition: none;[\s\S]*?transform: none;/,
    );
  });
});
