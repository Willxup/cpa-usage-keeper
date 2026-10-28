// 时间身份与显示风格分离：调用方保留原 locale、精度和 fallback，这里只处理响应时区与回拨歧义。
export const getIntlTimeZone = (timezone: string | undefined): string | undefined => {
  const trimmed = timezone?.trim();
  return !trimmed || trimmed === 'Local' ? undefined : trimmed;
};

export interface ZonedParts {
  year: number;
  month: number;
  day: number;
  hour: number;
  minute: number;
  second: number;
}

export function createZonedPartsReader(timeZone: string | undefined) {
  const formatter = new Intl.DateTimeFormat('en-CA', {
    timeZone, year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23',
  });
  return (timestampMs: number): ZonedParts => {
    const parts = Object.fromEntries(formatter.formatToParts(new Date(timestampMs))
      .filter((part) => part.type !== 'literal').map((part) => [part.type, Number(part.value)]));
    return { year: parts.year, month: parts.month, day: parts.day, hour: parts.hour, minute: parts.minute, second: parts.second };
  };
}

const pad2 = (value: number) => String(value).padStart(2, '0');

// 仅同一当地日期和钟面、不同 instant 且不同 offset 才补后缀；跨日相同 HH:mm 不算歧义。
// 每批只构造一次 Intl formatter，返回的数组与输入顺序一一对应，不改 bucket 身份。
export function disambiguateTimeLabels(
  values: readonly string[], timezone: string | undefined, format: (value: string, index: number) => string,
  precision: 'hours' | 'minutes' | 'seconds' = 'minutes',
): string[] {
  const labels = values.map(format);
  // 未知服务端地区时区时沿用调用方原 fallback，不用浏览器地区推断后缀。
  const resolvedTimezone = getIntlTimeZone(timezone);
  if (!resolvedTimezone) return labels;
  try {
    const readParts = createZonedPartsReader(resolvedTimezone);
    const clocks = values.map((value) => {
      const instant = Date.parse(value);
      if (!Number.isFinite(instant)) return null;
      const p = readParts(instant);
      const wall = Date.UTC(p.year, p.month - 1, p.day, p.hour, p.minute, p.second);
      const offset = Math.round((wall - Math.floor(instant / 1000) * 1000) / 60_000);
      const clock = Date.UTC(p.year, p.month - 1, p.day, p.hour, precision === 'hours' ? 0 : p.minute, precision === 'seconds' ? p.second : 0);
      return { wall: clock, offset };
    });
    const offsetsByClock = new Map<number, Set<number>>();
    clocks.forEach((clock) => {
      if (!clock) return;
      const offsets = offsetsByClock.get(clock.wall) ?? new Set<number>();
      offsets.add(clock.offset);
      offsetsByClock.set(clock.wall, offsets);
    });
    return labels.map((label, index) => {
      const clock = clocks[index];
      if (!clock || (offsetsByClock.get(clock.wall)?.size ?? 0) < 2) return label;
      const absolute = Math.abs(clock.offset);
      return `${label} UTC${clock.offset < 0 ? '-' : '+'}${pad2(Math.floor(absolute / 60))}:${pad2(absolute % 60)}`;
    });
  } catch {
    return labels;
  }
}
