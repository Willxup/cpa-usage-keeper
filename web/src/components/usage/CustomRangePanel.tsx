import { useLayoutEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { UsageCustomRange, UsageCustomRangeUnit } from '@/lib/types';
import { Button } from '@/components/ui/Button';
import { IconChevronLeft } from '@/components/ui/icons';
import {
  buildCustomDaySlots,
  buildCustomHourSlots,
  buildCustomWeekdayLabels,
  buildDefaultCustomRange,
  formatCustomRangeLabel,
  type UsageCustomRangeSlot,
} from '@/utils/usage/customRange';
import styles from './TimeRangeControl.module.scss';

type CustomPickerView = 'summary' | 'day' | 'hour';
type CustomEndpoint = 'start' | 'end';

interface CustomRangePanelProps {
  value: UsageCustomRange;
  timeZone: string;
  locale?: string;
  anchorMs: number;
  onChange: (value: UsageCustomRange) => void;
  onApply: () => void;
  onCancel: () => void;
}

const parseDayKey = (value: string): Date => {
  const [year, month, day] = value.split('-').map(Number);
  return new Date(Date.UTC(year, month - 1, day));
};

const formatEndpoint = (value: string, unit: UsageCustomRangeUnit, timeZone: string, locale?: string): string => {
  if (unit === 'day') {
    return new Intl.DateTimeFormat(locale, { year: 'numeric', month: 'short', day: 'numeric', timeZone: 'UTC' })
      .format(parseDayKey(value));
  }
  return new Intl.DateTimeFormat(locale, {
    timeZone,
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    hourCycle: 'h23',
  }).format(new Date(value));
};

const formatCalendarDay = (value: string, locale?: string): string => new Intl.DateTimeFormat(locale, {
  weekday: 'long',
  year: 'numeric',
  month: 'long',
  day: 'numeric',
  timeZone: 'UTC',
}).format(parseDayKey(value));

const monthKey = (value: string): string => value.slice(0, 7);

interface CalendarCell {
  value: string;
  outsideMonth: boolean;
}

const buildCalendarCells = (visibleMonth: string): CalendarCell[] => {
  const [year, month] = visibleMonth.split('-').map(Number);
  const firstDay = new Date(Date.UTC(year, month - 1, 1));
  const gridStart = new Date(firstDay);
  gridStart.setUTCDate(firstDay.getUTCDate() - firstDay.getUTCDay());

  // 固定生成完整六周，让跨月范围在同一网格中连续显示。
  return Array.from({ length: 42 }, (_, index) => {
    const date = new Date(gridStart);
    date.setUTCDate(gridStart.getUTCDate() + index);
    const value = date.toISOString().slice(0, 10);
    return { value, outsideMonth: monthKey(value) !== visibleMonth };
  });
};

function CustomRangeSummary({
  value,
  timeZone,
  locale,
  slotCount,
  onUnitChange,
  onEdit,
  onApply,
  onCancel,
}: {
  value: UsageCustomRange;
  timeZone: string;
  locale?: string;
  slotCount: number;
  onUnitChange: (unit: UsageCustomRangeUnit) => void;
  onEdit: (endpoint: CustomEndpoint) => void;
  onApply: () => void;
  onCancel: () => void;
}) {
  const { t } = useTranslation();
  return (
    <div className={styles.customSummary} data-custom-range-summary>
      <div className={styles.customUnitSelector} role="group" aria-label={t('usage_stats.range_custom_unit')}>
        {(['hour', 'day'] as const).map((unit) => (
          <button
            key={unit}
            type="button"
            className={value.unit === unit ? styles.customUnitButtonActive : styles.customUnitButton}
            data-custom-unit={unit}
            aria-pressed={value.unit === unit}
            onClick={() => onUnitChange(unit)}
          >
            {t(unit === 'hour' ? 'usage_stats.range_unit_hour' : 'usage_stats.range_unit_day')}
          </button>
        ))}
      </div>
      <div className={styles.customEndpointGrid}>
        {(['start', 'end'] as const).map((endpoint, index) => (
          <div key={endpoint} className={styles.customEndpointGroup}>
            <button
              type="button"
              className={styles.customEndpointCard}
              data-custom-endpoint={endpoint}
              onClick={() => onEdit(endpoint)}
            >
              <small>{t(endpoint === 'start' ? 'usage_stats.range_custom_start' : 'usage_stats.range_custom_end')}</small>
              <strong>{formatEndpoint(value[endpoint], value.unit, timeZone, locale)}</strong>
              <span>{endpoint === 'end' && value.end === value.start ? t('usage_stats.range_custom_same_slot') : t('usage_stats.range_custom_edit')}</span>
            </button>
            {index === 0 && <span className={styles.customEndpointArrow} aria-hidden="true">→</span>}
          </div>
        ))}
      </div>
      <div className={styles.customRangeMeta}>
        <span>{t(value.unit === 'hour' ? 'usage_stats.range_custom_hours_count' : 'usage_stats.range_custom_days_count', { count: slotCount })}</span>
        <strong>{timeZone}</strong>
      </div>
      <div className={styles.customSummaryActions}>
        <Button type="button" variant="secondary" size="sm" className={styles.customRangeAction} data-custom-summary-cancel onClick={onCancel}>
          {t('common.cancel')}
        </Button>
        <Button type="button" variant="primary" size="sm" className={styles.customRangeAction} data-custom-summary-apply data-custom-range-apply onClick={onApply}>
          {t('common.apply')}
        </Button>
      </div>
    </div>
  );
}

export function CustomRangePanel({ value, timeZone, locale, anchorMs, onChange, onApply, onCancel }: CustomRangePanelProps) {
  const { t } = useTranslation();
  const [view, setView] = useState<CustomPickerView>('summary');
  const [activeEndpoint, setActiveEndpoint] = useState<CustomEndpoint>('start');
  const daySlots = useMemo(() => buildCustomDaySlots({ nowMs: anchorMs, timeZone, locale }), [anchorMs, locale, timeZone]);
  const hourSlots = useMemo(() => buildCustomHourSlots({ nowMs: anchorMs, timeZone, locale }), [anchorMs, locale, timeZone]);
  const weekdayLabels = useMemo(() => buildCustomWeekdayLabels(locale), [locale]);
  const slots = value.unit === 'hour' ? hourSlots : daySlots;
  const startIndex = slots.findIndex((slot) => slot.value === value.start);
  const endIndex = slots.findIndex((slot) => slot.value === value.end);
  const slotCount = Math.max(endIndex - startIndex + 1, 0);
  const allowedMonths = useMemo(() => [...new Set(daySlots.map((slot) => monthKey(slot.value)))], [daySlots]);
  const allowedDayValues = useMemo(() => new Set(daySlots.map((slot) => slot.value)), [daySlots]);
  const activeDay = value[activeEndpoint].slice(0, 10);
  const [visibleMonth, setVisibleMonth] = useState(monthKey(activeDay));
  const [pickerSnapshot, setPickerSnapshot] = useState<UsageCustomRange | null>(null);
  const startHourListRef = useRef<HTMLDivElement | null>(null);
  const endHourListRef = useRef<HTMLDivElement | null>(null);
  const calendarCells = useMemo(() => buildCalendarCells(visibleMonth), [visibleMonth]);

  useLayoutEffect(() => {
    if (view !== 'hour') return;
    // 只调整两个小时列表自身，避免 scrollIntoView 带动外层页面。
    [startHourListRef.current, endHourListRef.current].forEach((list) => {
      const selected = list?.querySelector<HTMLElement>('[data-custom-hour-selected]');
      if (!list || !selected) return;
      list.scrollTop = Math.max(0, selected.offsetTop - (list.clientHeight - selected.offsetHeight) / 2);
    });
  }, [view]);

  const handleUnitChange = (unit: UsageCustomRangeUnit) => {
    if (unit === value.unit) return;
    setView('summary');
    setActiveEndpoint('start');
    onChange(buildDefaultCustomRange({ unit, nowMs: anchorMs, timeZone }));
  };

  const handleEdit = (endpoint: CustomEndpoint) => {
    setPickerSnapshot({ ...value });
    setActiveEndpoint(endpoint);
    if (value.unit === 'day') {
      setVisibleMonth(monthKey(value[endpoint]));
    }
    setView(value.unit);
  };

  const handleEndpointChange = (endpoint: CustomEndpoint) => {
    setActiveEndpoint(endpoint);
    if (value.unit === 'day') {
      setVisibleMonth(monthKey(value[endpoint]));
    }
  };

  const handlePickerCancel = () => {
    if (pickerSnapshot) onChange(pickerSnapshot);
    setPickerSnapshot(null);
    setView('summary');
  };

  const handlePickerApply = () => {
    setPickerSnapshot(null);
    onApply();
  };

  const handleDaySelect = (day: string) => {
    if (activeEndpoint === 'start') {
      onChange({ ...value, start: day, end: value.end < day ? day : value.end });
      setActiveEndpoint('end');
      setVisibleMonth(monthKey(value.end < day ? day : value.end));
      return;
    }
    onChange({ ...value, start: value.start > day ? day : value.start, end: day });
  };

  const handleHourStartSelect = (slot: UsageCustomRangeSlot, index: number) => {
    if (index > hourSlots.length - 5) return;
    const currentEndIndex = hourSlots.findIndex((item) => item.value === value.end);
    const nextEndIndex = Math.max(currentEndIndex, index + 4);
    onChange({ unit: 'hour', start: slot.value, end: hourSlots[nextEndIndex].value });
  };

  const handleHourEndSelect = (slot: UsageCustomRangeSlot, index: number) => {
    const currentStartIndex = hourSlots.findIndex((item) => item.value === value.start);
    if (index < currentStartIndex + 4) return;
    onChange({ ...value, end: slot.value });
  };

  if (view === 'summary') {
    return (
      <CustomRangeSummary
        value={value}
        timeZone={timeZone}
        locale={locale}
        slotCount={slotCount}
        onUnitChange={handleUnitChange}
        onEdit={handleEdit}
        onApply={onApply}
        onCancel={onCancel}
      />
    );
  }

  const endpointCards = (
    <div className={styles.customPickerEndpoints}>
      {(['start', 'end'] as const).map((endpoint) => (
        <button
          key={endpoint}
          type="button"
          className={activeEndpoint === endpoint ? styles.customPickerEndpointActive : styles.customPickerEndpoint}
          data-custom-picker-endpoint={endpoint}
          aria-pressed={activeEndpoint === endpoint}
          onClick={() => handleEndpointChange(endpoint)}
        >
          <small>{t(endpoint === 'start' ? 'usage_stats.range_custom_start' : 'usage_stats.range_custom_end')}</small>
          <strong>{formatEndpoint(value[endpoint], value.unit, timeZone, locale)}</strong>
        </button>
      ))}
    </div>
  );

  return (
    <div className={styles.customPicker} data-custom-day-picker={view === 'day' ? '' : undefined} data-custom-hour-picker={view === 'hour' ? '' : undefined}>
      <div className={styles.customPickerHeader}>
        <button type="button" className={styles.customBackButton} onClick={handlePickerCancel} aria-label={t('common.back')}>
          <IconChevronLeft size={15} />
        </button>
        <span>
          <strong>{t(view === 'day' ? 'usage_stats.range_custom_select_days' : 'usage_stats.range_custom_select_hours')}</strong>
          <small>{timeZone}</small>
        </span>
      </div>
      {endpointCards}

      {view === 'day' ? (
        <div className={styles.customCalendar} data-custom-calendar-month={visibleMonth}>
          <div className={styles.customCalendarHeader}>
            <button
              type="button"
              disabled={allowedMonths.indexOf(visibleMonth) <= 0}
              onClick={() => setVisibleMonth(allowedMonths[allowedMonths.indexOf(visibleMonth) - 1])}
              aria-label={t('usage_stats.range_custom_previous_month')}
            ><IconChevronLeft size={14} /></button>
            <strong>{new Intl.DateTimeFormat(locale, { year: 'numeric', month: 'long', timeZone: 'UTC' }).format(parseDayKey(`${visibleMonth}-01`))}</strong>
            <button
              type="button"
              disabled={allowedMonths.indexOf(visibleMonth) >= allowedMonths.length - 1}
              className={styles.customNextMonthButton}
              onClick={() => setVisibleMonth(allowedMonths[allowedMonths.indexOf(visibleMonth) + 1])}
              aria-label={t('usage_stats.range_custom_next_month')}
            ><IconChevronLeft size={14} /></button>
          </div>
          <div className={styles.customCalendarWeekdays} aria-hidden="true">
            {weekdayLabels.map((day, index) => <span key={`${day}-${index}`}>{day}</span>)}
          </div>
          <div className={styles.customCalendarGrid}>
            {calendarCells.map(({ value: day, outsideMonth }, index) => {
              const allowed = allowedDayValues.has(day);
              const selected = day === value.start || day === value.end;
              const inRange = allowed && day >= value.start && day <= value.end;
              const previousDay = calendarCells[index - 1]?.value;
              const nextDay = calendarCells[index + 1]?.value;
              const previousInRange = previousDay !== undefined
                && allowedDayValues.has(previousDay)
                && previousDay >= value.start
                && previousDay <= value.end;
              const nextInRange = nextDay !== undefined
                && allowedDayValues.has(nextDay)
                && nextDay >= value.start
                && nextDay <= value.end;
              // 色带只在真实区间端点或每周换行处收口，不在月份边界人为断开。
              const rangeRowStart = inRange && (index % 7 === 0 || !previousInRange);
              const rangeRowEnd = inRange && (index % 7 === 6 || !nextInRange);
              const rangeState = day === value.start && day === value.end
                ? t('usage_stats.range_custom_day_start_end')
                : day === value.start
                  ? t('usage_stats.range_custom_day_start')
                  : day === value.end
                    ? t('usage_stats.range_custom_day_end')
                    : inRange
                      ? t('usage_stats.range_custom_day_in_range')
                      : '';
              const accessibleDate = formatCalendarDay(day, locale);
              return (
                <button
                  key={day}
                  type="button"
                  data-custom-calendar-cell={day}
                  data-custom-day={allowed ? day : undefined}
                  data-custom-outside-month={outsideMonth ? '' : undefined}
                  data-custom-in-range={inRange ? '' : undefined}
                  data-custom-range-row-start={rangeRowStart ? '' : undefined}
                  data-custom-range-row-end={rangeRowEnd ? '' : undefined}
                  aria-label={rangeState ? `${accessibleDate}, ${rangeState}` : accessibleDate}
                  aria-pressed={selected}
                  className={`${styles.customCalendarDay} ${outsideMonth ? styles.customCalendarDayOutsideMonth : ''} ${inRange ? styles.customCalendarDayInRange : ''} ${rangeRowStart ? styles.customCalendarRangeRowStart : ''} ${rangeRowEnd ? styles.customCalendarRangeRowEnd : ''} ${selected ? styles.customCalendarDaySelected : ''}`.trim()}
                  disabled={!allowed}
                  onClick={() => handleDaySelect(day)}
                ><span>{Number(day.slice(-2))}</span></button>
              );
            })}
          </div>
        </div>
      ) : (
        <div className={styles.customHourColumns}>
          <div className={styles.customHourColumn}>
            <strong>{t('usage_stats.range_custom_start')}</strong>
            <div ref={startHourListRef} className={styles.customHourList} data-custom-hour-list="start">
              {hourSlots.map((slot, index) => (
                <button
                  key={slot.value}
                  type="button"
                  data-custom-hour-start={slot.value}
                  data-custom-hour-selected={slot.value === value.start ? '' : undefined}
                  aria-pressed={slot.value === value.start}
                  disabled={index > hourSlots.length - 5}
                  className={slot.value === value.start ? styles.customHourOptionActive : styles.customHourOption}
                  onClick={() => handleHourStartSelect(slot, index)}
                ><span>{slot.dateLabel}</span><strong>{slot.label}</strong></button>
              ))}
            </div>
          </div>
          <div className={styles.customHourColumn}>
            <strong>{t('usage_stats.range_custom_end')}</strong>
            <div ref={endHourListRef} className={styles.customHourList} data-custom-hour-list="end">
              {hourSlots.map((slot, index) => (
                <button
                  key={slot.value}
                  type="button"
                  data-custom-hour-end={slot.value}
                  data-custom-hour-selected={slot.value === value.end ? '' : undefined}
                  aria-pressed={slot.value === value.end}
                  disabled={index < startIndex + 4}
                  className={slot.value === value.end ? styles.customHourOptionActive : styles.customHourOption}
                  onClick={() => handleHourEndSelect(slot, index)}
                ><span>{slot.dateLabel}</span><strong>{slot.current ? t('usage_stats.range_custom_now') : slot.label}</strong></button>
              ))}
            </div>
          </div>
        </div>
      )}

      <div className={styles.customPickerFooter}>
        <span>{formatCustomRangeLabel(value, { locale, timeZone })}</span>
        <div className={styles.customPickerActions}>
          <Button type="button" variant="secondary" size="sm" className={styles.customRangeAction} data-custom-picker-cancel onClick={handlePickerCancel}>
            {t('common.cancel')}
          </Button>
          <Button type="button" variant="primary" size="sm" className={styles.customRangeAction} data-custom-range-apply onClick={handlePickerApply}>
            {t('common.apply')}
          </Button>
        </div>
      </div>
    </div>
  );
}
