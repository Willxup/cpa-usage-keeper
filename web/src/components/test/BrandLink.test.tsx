import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { BrandLink } from '../BrandLink';

describe('BrandLink', () => {
  it('renders the Keeper icon and wordmark', () => {
    const html = renderToStaticMarkup(<BrandLink />);

    expect(html).toContain('<img');
    expect(html).toContain('alt=""');
    expect(html).toContain('aria-hidden="true"');
    expect(html).toContain('>KEEPER</span>');
    expect(html).not.toContain('CPA Usage Keeper');
  });
});
