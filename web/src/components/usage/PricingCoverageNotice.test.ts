import { describe, expect, it } from 'vitest';
import { normalizeUnpricedModels } from './PricingCoverageNotice';

describe('normalizeUnpricedModels', () => {
  it('returns a stable unique list of exact model identifiers', () => {
    expect(normalizeUnpricedModels([' model-z ', 'model-a', '', 'model-z'])).toEqual([
      'model-a',
      'model-z',
    ]);
  });
});
