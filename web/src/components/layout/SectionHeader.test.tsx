import { readFileSync } from 'node:fs';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { SectionHeader } from './SectionHeader';

const styles = readFileSync(new URL('./SectionHeader.module.scss', import.meta.url), 'utf8');

describe('SectionHeader', () => {
  it('renders section and subsection heading levels explicitly', () => {
    const sectionHtml = renderToStaticMarkup(
      <SectionHeader title="Credentials" description="Manage access" meta={<span>4</span>} actions={<button type="button">Refresh</button>} />,
    );
    const subsectionHtml = renderToStaticMarkup(
      <SectionHeader headingLevel={3} title="Health" size="subsection" />,
    );

    expect(sectionHtml).toContain('<h2');
    expect(sectionHtml).toContain('Manage access');
    expect(sectionHtml).toContain('Refresh');
    expect(subsectionHtml).toContain('<h3');
  });

  it('uses shared semantic typography and control spacing', () => {
    expect(styles).toContain('font-size: var(--type-section-title-size);');
    expect(styles).toContain('font-size: var(--type-subsection-title-size);');
    expect(styles).toContain('font-size: var(--type-body-size);');
    expect(styles).toContain('gap: var(--control-gap);');
  });
});
