import type { ArcElement, Chart, Plugin } from 'chart.js';

type ArcOptionsRestore = { element: ArcElement; options: ArcElement['options'] };
const originalOptions = new WeakMap<Chart, ArcOptionsRestore[]>();

export const compositionGeometryPlugin: Plugin<'doughnut'> = {
  id: 'analysis-composition-geometry',
  beforeDatasetDraw(chart, { meta }) {
    const restore: ArcOptionsRestore[] = [];
    for (const element of meta.data as ArcElement[]) {
      const options = element.options;
      // 极短弧会被外移与圆角挤到角度反转；Canvas 会把负角度差绕成接近整圈。
      // 为当前帧的内弧留出绘制空间，正常大扇区仍使用原来的外移和圆角。
      const arcLength = Math.max(0, element.endAngle - element.startAngle) * Math.max(0, element.innerRadius);
      const offset = Math.min(options.offset, arcLength);
      const borderRadius = typeof options.borderRadius === 'number'
        ? Math.min(options.borderRadius, arcLength / 4)
        : options.borderRadius;
      if (offset !== options.offset || borderRadius !== options.borderRadius) {
        restore.push({ element, options });
        element.options = { ...options, offset, borderRadius };
      }
    }
    originalOptions.set(chart, restore);
  },
  afterDatasetDraw(chart) {
    // 仅调整本次绘制；恢复 Chart.js 持有的选项对象，让悬浮与动画继续使用原有状态。
    for (const { element, options } of originalOptions.get(chart) ?? []) element.options = options;
    originalOptions.delete(chart);
  },
};
