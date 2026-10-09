import type { ArcElement, Plugin } from 'chart.js';

type CompositionLabel = { name: string; share: string };
type PositionedLabel = CompositionLabel & {
  anchorX: number;
  anchorY: number;
  elbowX: number;
  textX: number;
  y: number;
  side: -1 | 1;
  value: number;
  active: boolean;
};

const LABEL_GAP = 32;
const EDGE_PADDING = 8;

function fitLabel(ctx: CanvasRenderingContext2D, text: string, width: number): string {
  if (ctx.measureText(text).width <= width) return text;
  const characters = Array.from(text);
  while (characters.length > 0 && ctx.measureText(`${characters.join('')}…`).width > width) {
    characters.pop();
  }
  return characters.length > 0 ? `${characters.join('')}…` : '…';
}

export function createCompositionLabelsPlugin(labels: CompositionLabel[], color: string): Plugin<'doughnut'> {
  return {
    id: 'analysis-composition-labels',
    afterDatasetsDraw(chart) {
      const { ctx, width, height } = chart;
      const positions: PositionedLabel[] = [];
      chart.getDatasetMeta(0).data.forEach((element, index) => {
        const label = labels[index];
        const value = Number(chart.data.datasets[0]?.data[index]);
        if (!label || !(value > 0) || !chart.getDataVisibility(index)) return;
        const arc = (element as ArcElement).getProps(['x', 'y', 'outerRadius', 'startAngle', 'endAngle'], true);
        if (arc.x === null || arc.y === null || !(arc.outerRadius > 0) || arc.endAngle <= arc.startAngle) return;
        const angle = (arc.startAngle + arc.endAngle) / 2;
        const side = Math.cos(angle) >= 0 ? 1 : -1;
        positions.push({
          ...label,
          value,
          active: (element as ArcElement).options.offset > 0 || chart.getActiveElements().some((item) => item.index === index),
          side,
          anchorX: arc.x + Math.cos(angle) * arc.outerRadius,
          anchorY: arc.y + Math.sin(angle) * arc.outerRadius,
          elbowX: arc.x + side * (arc.outerRadius + 8),
          textX: arc.x + side * (arc.outerRadius + 18),
          y: arc.y + Math.sin(angle) * (arc.outerRadius + 12),
        });
      });

      ctx.save();
      ctx.font = `600 11px ${chart.options.font?.family ?? 'sans-serif'}`;
      ctx.fillStyle = color;
      ctx.strokeStyle = color;
      ctx.lineWidth = 1;
      ctx.textBaseline = 'middle';

      // 左右分别按高度排布，再从底部回推，避免集中在小扇区的标签重叠或越界。
      for (const side of [-1, 1] as const) {
        const top = EDGE_PADDING + 10;
        const bottom = height - EDGE_PADDING - 10;
        // 全量扇区仍参与绘制，外围只放得下的标签优先展示选中项与大项。
        const capacity = Math.max(1, Math.floor((bottom - top) / LABEL_GAP) + 1);
        const column = positions.filter((label) => label.side === side)
          .sort((a, b) => Number(b.active) - Number(a.active) || b.value - a.value)
          .slice(0, capacity).sort((a, b) => a.y - b.y);
        column.forEach((label, index) => {
          label.y = Math.max(top, label.y, index > 0 ? column[index - 1].y + LABEL_GAP : top);
        });
        for (let index = column.length - 1; index >= 0; index -= 1) {
          const ceiling = index === column.length - 1 ? bottom : column[index + 1].y - LABEL_GAP;
          column[index].y = Math.min(column[index].y, ceiling);
        }

        ctx.textAlign = side === 1 ? 'left' : 'right';
        for (const label of column) {
          const availableWidth = (side === 1 ? width - label.textX : label.textX) - EDGE_PADDING;
          ctx.globalAlpha = 0.45;
          ctx.beginPath();
          ctx.moveTo(label.anchorX, label.anchorY);
          ctx.lineTo(label.elbowX, label.y);
          ctx.lineTo(label.textX - side * 4, label.y);
          ctx.stroke();
          ctx.globalAlpha = 1;
          ctx.fillText(fitLabel(ctx, label.name, availableWidth), label.textX, label.y - 7);
          ctx.globalAlpha = 0.7;
          ctx.fillText(label.share, label.textX, label.y + 7, Math.max(1, availableWidth));
        }
      }
      ctx.restore();
    },
  };
}
