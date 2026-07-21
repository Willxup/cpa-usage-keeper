// 样本越多，对绿色状态的成功率要求越高；10 次以内以 90% 为基线，1000 次后封顶 99%。
export function healthGreenThreshold(total: number): number {
  return Math.min(0.99, 0.9 + 0.045 * Math.max(0, Math.log10(total / 10)));
}
