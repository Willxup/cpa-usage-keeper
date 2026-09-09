// 模型列表和下拉框共用的系列优先级；调整此处即可改变优先展示顺序。
const MODEL_FAMILY_PRIORITY: readonly string[] = ['gpt', 'claude', 'gemini'];

const modelNameCollator = new Intl.Collator('en', {
  numeric: true,
  sensitivity: 'base',
});

const modelSortKey = (displayName: string) => {
  // 仅用最后一个 / 后的模型名生成排序键，完整 ID 仍用于展示、选项值和等值兜底。
  const modelName = displayName.slice(displayName.lastIndexOf('/') + 1).trim() || displayName;
  const family = modelName.match(/^[a-z]+/i)?.[0].toLowerCase() ?? '';
  const priority = MODEL_FAMILY_PRIORITY.indexOf(family);

  // Claude 的旧命名把子系列放在版本后；4-6 与 4.6 按同一版本比较，日期仍留在后缀。
  const sortableName = modelName
    .replace(/^claude-(\d+(?:[.-]\d+)*)-(haiku|opus|sonnet)(?=-|$)/i, 'claude-$2-$1')
    .replace(/^(claude-(?:haiku|opus|sonnet)-\d+)-(\d{1,2})(?=-|$)/i, '$1.$2');
  const parts = sortableName.match(/^(\D*?)(\d+(?:\.\d+)*)(.*)$/);

  return {
    priority: priority < 0 ? MODEL_FAMILY_PRIORITY.length : priority,
    series: (parts?.[1] ?? sortableName).replace(/[-_.\s]+$/, ''),
    version: parts?.[2] ?? '',
    suffix: parts?.[3] ?? '',
  };
};

export const compareModelNames = (left: string, right: string): number => {
  const leftDisplayName = left.trim() || '-';
  const rightDisplayName = right.trim() || '-';
  const leftKey = modelSortKey(leftDisplayName);
  const rightKey = modelSortKey(rightDisplayName);

  // 系列与子系列升序、版本降序、后缀自然升序；空后缀让基础型号排在变体前。
  const order = leftKey.priority - rightKey.priority
    || modelNameCollator.compare(leftKey.series, rightKey.series)
    || modelNameCollator.compare(rightKey.version, leftKey.version)
    || modelNameCollator.compare(leftKey.suffix, rightKey.suffix);
  if (order !== 0) return order;

  // 大小写或前导零等自然比较等值时，保留精确名称兜底，避免来源顺序影响展示。
  if (leftDisplayName !== rightDisplayName) {
    return leftDisplayName > rightDisplayName ? -1 : 1;
  }
  if (left === right) return 0;
  return left > right ? -1 : 1;
};
