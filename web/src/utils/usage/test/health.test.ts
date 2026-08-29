import { describe, expect, it } from 'vitest';
import { healthGreenThreshold } from '../health';

describe('healthGreenThreshold', () => {
  it('raises the green threshold with request volume and caps it at 99 percent', () => {
    expect(healthGreenThreshold(1)).toBe(0.9);
    expect(healthGreenThreshold(10)).toBe(0.9);
    expect(healthGreenThreshold(100)).toBeCloseTo(0.945, 10);
    expect(healthGreenThreshold(1_000)).toBe(0.99);
    expect(healthGreenThreshold(10_000)).toBe(0.99);
  });
});
