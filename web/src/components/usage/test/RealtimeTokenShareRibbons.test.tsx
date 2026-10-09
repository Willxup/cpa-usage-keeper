import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { RealtimeTokenShareRibbons } from '../RealtimeTokenShareRibbons';

describe('RealtimeTokenShareRibbons', () => {
  it('keeps complete labels, the exact small share, unknown cost, and synthetic Other separate', () => {
    const label = 'very-long-authentication-file-name-without-any-spaces.json';
    const html = renderToStaticMarkup(
      <RealtimeTokenShareRibbons loading={false} items={[
        { key: '__realtime_others__', label, tokens: 997, requests: 7, share: 99.7, cost: 1 },
        { key: 'tiny', label: 'tiny', tokens: 3, requests: 1, share: 0.3, cost: null },
        { key: 'b', label: 'b', tokens: 0, requests: 0, share: 0 },
        { key: 'c', label: 'c', tokens: 0, requests: 0, share: 0 },
        { key: 'd', label: 'd', tokens: 0, requests: 0, share: 0 },
        { key: '__realtime_others__', label: 'Other', tokens: 0, requests: 0, share: 0 },
      ]} />,
    );
    expect(html).toContain(label);
    expect(html).toContain('0.30%');
    expect(html).toContain('99.70%');
    expect(html).toContain('data-ribbon-row="5"');
    expect(html).toContain('data-ribbon-row="0"');
    expect(html).toContain('>—</span>');
  });
});
