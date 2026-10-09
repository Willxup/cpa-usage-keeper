import type { Chart } from 'chart.js';
import { describe, expect, it, vi } from 'vitest';
import { createCompositionLabelsPlugin } from '../compositionLabels';

function drawLabels(width: number, values: number[], hiddenIndex = -1, activeIndex = -1) {
  const drawn: Array<{ text: string; x: number; y: number; align: string; width: number }> = [];
  const ctx = {
    textAlign: 'left',
    save: vi.fn(), restore: vi.fn(), beginPath: vi.fn(),
    moveTo: vi.fn(), lineTo: vi.fn(), stroke: vi.fn(),
    measureText: (text: string) => ({ width: Array.from(text).length * 6 }),
    fillText(text: string, x: number, y: number, maxWidth = Infinity) {
      drawn.push({ text, x, y, align: this.textAlign, width: Math.min(this.measureText(text).width, maxWidth) });
    },
  };
  let angle = -Math.PI / 2;
  const total = values.reduce((sum, value) => sum + value, 0);
  const elements = values.map((value, index) => {
    const startAngle = angle;
    angle += total > 0 ? value / total * Math.PI * 2 : 0;
    const props = { x: width / 2, y: 125, outerRadius: Math.min(105, width * 0.23), startAngle, endAngle: angle };
    return { getProps: () => props, options: { offset: index === activeIndex ? 8 : 0 } };
  });
  const chart = {
    ctx, width, height: 250,
    options: { font: { family: 'sans-serif' } },
    data: { datasets: [{ data: values }] },
    getDatasetMeta: () => ({ data: elements }),
    getDataVisibility: (index: number) => index !== hiddenIndex,
    getActiveElements: () => [],
  } as unknown as Chart<'doughnut'>;
  const plugin = createCompositionLabelsPlugin(values.map((_, index) => ({
    name: `${index}-long-model-or-credential-name-测试名称`,
    share: `${index + 1}.00%`,
  })), '#123456');
  plugin.afterDatasetsDraw?.(chart, { cancelable: false }, {});
  return { drawn, ctx };
}

describe('composition chart labels', () => {
  it.each([224, 280, 420, 700])('keeps visible labels separated at width %s even with tiny adjacent slices', (width) => {
    const { drawn, ctx } = drawLabels(width, [995, 1, 1, 1, 1, 1]);
    expect(drawn).toHaveLength(12);
    expect(drawn.filter((label) => label.text.endsWith('%'))).toHaveLength(6);
    expect(ctx.stroke).toHaveBeenCalledTimes(6);
    for (const label of drawn) {
      const textWidth = label.width;
      const left = label.align === 'left' ? label.x : label.x - textWidth;
      expect(left).toBeGreaterThanOrEqual(0);
      expect(left + textWidth).toBeLessThanOrEqual(width);
      expect(label.y).toBeGreaterThanOrEqual(8);
      expect(label.y).toBeLessThanOrEqual(242);
    }
    for (const align of ['left', 'right']) {
      const rows = drawn.filter((label) => label.align === align).sort((a, b) => a.y - b.y);
      rows.slice(1).forEach((label, index) => expect(label.y - rows[index].y).toBeGreaterThanOrEqual(14));
    }
  });

  it('keeps the selected small item visible when a crowded chart limits labels', () => {
    const values = [1000, ...Array.from({ length: 30 }, () => 1)];
    expect(drawLabels(420, values).drawn.some((label) => label.text.startsWith('30-'))).toBe(false);
    expect(drawLabels(420, values, -1, 30).drawn.some((label) => label.text.startsWith('30-'))).toBe(true);
  });

  it('omits zero-value and hidden slices and supports an empty chart', () => {
    expect(drawLabels(280, [1, 0, 1], 2).drawn).toHaveLength(2);
    expect(drawLabels(280, [0]).drawn).toHaveLength(0);
    expect(drawLabels(280, []).drawn).toHaveLength(0);
  });
});
