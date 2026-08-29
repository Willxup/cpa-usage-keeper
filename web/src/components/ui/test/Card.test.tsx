import { resolve } from 'node:path';
import { renderToStaticMarkup } from 'react-dom/server';
import { compile } from 'sass';
import { describe, expect, it } from 'vitest';
import { Card } from '../Card';

const componentsCSS = compile(resolve(process.cwd(), 'src/styles/components.scss')).css;

describe('Card', () => {
  it('renders the shared heading contract and flush surface variant', () => {
    const html = renderToStaticMarkup(
      <Card
        title="Title"
        subtitle="Description"
        titleMeta={<span>3 items</span>}
        extra={<button type="button">Action</button>}
        variant="flush"
      >
        Body
      </Card>,
    );

    expect(html).toContain('class="card card-flush"');
    expect(html).toContain('class="card-header"');
    expect(html).toContain('class="keeper-card-heading"');
    expect(html).toContain('class="keeper-card-title-track"');
    expect(html).toContain('class="keeper-card-title"');
    expect(html).toContain('class="keeper-card-title-meta"');
    expect(html).toContain('class="keeper-card-subtitle"');
    expect(html).toContain('class="keeper-card-actions"');
    expect(html).toContain('>Description</p>');
    expect(html).toContain('>Body</div>');
  });

  it('preserves heading and paragraph semantics for the shared card copy', () => {
    const html = renderToStaticMarkup(
      <Card title="Title" subtitle="Description">
        Body
      </Card>,
    );

    expect(html).toContain('<h3 class="keeper-card-title">Title</h3>');
    expect(html).toContain('<p class="keeper-card-subtitle">Description</p>');
  });

  it('stacks card header actions below the heading at the mobile breakpoint', () => {
    expect(componentsCSS).toMatch(
      /@media \(max-width: 768px\) \{[\s\S]*?\.card-header \{[^}]*grid-template-columns: minmax\(0, 1fr\);/,
    );
  });

  it('keeps the default surface free of the flush modifier', () => {
    const html = renderToStaticMarkup(<Card>Body</Card>);

    expect(html).toContain('class="card"');
  });
});
