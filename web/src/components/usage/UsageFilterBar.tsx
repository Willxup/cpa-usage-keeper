import { useMemo, useState } from 'react';
import dayjs, { type Dayjs } from 'dayjs';
import { Button, DatePicker, Form, Popover, Select, Space } from 'antd';
import { CheckOutlined, ClockCircleOutlined, DownOutlined } from '@ant-design/icons';
import { useTranslation } from 'react-i18next';
import type { CpaApiKeyOption } from '@/lib/types';
import type { UsageTimeRange } from '@/utils/usage';
import styles from './UsageFilterBar.module.scss';

export interface UsageFilterBarOption<T extends string = string> {
  value: T;
  label: string;
}

export interface UsageAutoRefreshOption {
  value: number;
  label: string;
}

export interface UsageFilterBarProps {
  apiKeyOptions: CpaApiKeyOption[];
  selectedApiKeyId: string;
  onApiKeyChange: (id: string) => void;
  timeRange: UsageTimeRange;
  timeRangeOptions: UsageFilterBarOption<UsageTimeRange>[];
  onTimeRangeChange: (range: UsageTimeRange) => void;
  customTimeRange: { start: string; end: string };
  onCustomTimeRangeChange: (range: { start: string; end: string }) => void;
  customDateRangeBounds: { min: string; max: string };
  customRangeError?: string;
  customRangeHint?: string;
  showApiKeyFilter?: boolean;
  showAutoRefresh?: boolean;
  autoRefreshInterval?: number;
  autoRefreshOptions?: UsageAutoRefreshOption[];
  onAutoRefreshChange?: (value: number) => void;
  disabled?: boolean;
  layout?: 'inline' | 'vertical';
  /**
   * Compact mode drops the Form.Item text labels (relying on aria-label) and
   * gives each control a stable bounded width. Used inline in the page header
   * so API key labels remain readable without resizing after selection.
   */
  compact?: boolean;
  className?: string;
}

const DATE_FORMAT = 'YYYY-MM-DD';
const COMPACT_API_KEY_POPUP_WIDTH = 220;

type DateRangeValue = [Dayjs | null, Dayjs | null] | null;

const toDateValue = (value: string): Dayjs | null => {
  if (!value) return null;
  const date = dayjs(value, DATE_FORMAT, true);
  return date.isValid() ? date : null;
};

export function UsageFilterBar({
  apiKeyOptions,
  selectedApiKeyId,
  onApiKeyChange,
  timeRange,
  timeRangeOptions,
  onTimeRangeChange,
  customTimeRange,
  onCustomTimeRangeChange,
  customDateRangeBounds,
  customRangeError,
  customRangeHint,
  showApiKeyFilter = true,
  showAutoRefresh = false,
  autoRefreshInterval,
  autoRefreshOptions,
  onAutoRefreshChange,
  disabled,
  layout = 'inline',
  compact = false,
  className,
}: UsageFilterBarProps) {
  const { t } = useTranslation();
  const [rangePopoverOpen, setRangePopoverOpen] = useState(false);
  const [draftCustomTimeRange, setDraftCustomTimeRange] = useState(customTimeRange);
  const isVertical = layout === 'vertical';
  const apiKeySelectOptions = useMemo(
    () => [
      { value: '', label: t('usage_stats.api_key_filter_all') },
      ...apiKeyOptions.map((option) => ({ value: option.id, label: option.label })),
    ],
    [apiKeyOptions, t],
  );
  const draftCustomRangeValue = useMemo<DateRangeValue>(
    () => [toDateValue(draftCustomTimeRange.start), toDateValue(draftCustomTimeRange.end)],
    [draftCustomTimeRange.end, draftCustomTimeRange.start],
  );
  const minimumDate = useMemo(() => dayjs(customDateRangeBounds.min, DATE_FORMAT, true), [customDateRangeBounds.min]);
  const maximumDate = useMemo(() => dayjs(customDateRangeBounds.max, DATE_FORMAT, true), [customDateRangeBounds.max]);

  const selectedRangeLabel = useMemo(() => {
    if (timeRange === 'custom' && customTimeRange.start && customTimeRange.end) {
      return `${customTimeRange.start} – ${customTimeRange.end}`;
    }
    return timeRangeOptions.find((option) => option.value === timeRange)?.label ?? t('usage_stats.range_filter');
  }, [customTimeRange.end, customTimeRange.start, t, timeRange, timeRangeOptions]);
  const presetTimeRangeOptions = useMemo(
    () => timeRangeOptions.filter((option) => option.value !== 'custom'),
    [timeRangeOptions],
  );
  const canApplyCustomRange = Boolean(draftCustomTimeRange.start && draftCustomTimeRange.end);

  const handleRangePopoverOpenChange = (open: boolean) => {
    if (open) {
      setDraftCustomTimeRange(customTimeRange);
    }
    setRangePopoverOpen(open);
  };

  const handlePresetRangeChange = (range: UsageTimeRange) => {
    onTimeRangeChange(range);
    setRangePopoverOpen(false);
  };

  const handleDraftCustomRangeChange = (dates: DateRangeValue) => {
    setDraftCustomTimeRange({
      start: dates?.[0]?.format(DATE_FORMAT) ?? '',
      end: dates?.[1]?.format(DATE_FORMAT) ?? '',
    });
  };

  const handleApplyCustomRange = () => {
    if (!canApplyCustomRange) return;
    onCustomTimeRangeChange(draftCustomTimeRange);
    onTimeRangeChange('custom');
    setRangePopoverOpen(false);
  };

  // Compact controls fill the vertical drawer but keep stable bounded widths
  // when inline in the header so selecting a long label cannot shift the layout.
  const controlClassName = (compactClassName: string) => [
    compact ? compactClassName : '',
    isVertical ? styles.verticalControl : '',
  ].filter(Boolean).join(' ') || undefined;
  const apiKeyClassName = controlClassName(styles.apiKeyControl);
  const rangeClassName = controlClassName(styles.rangeControl);
  const autoRefreshClassName = controlClassName(styles.autoRefreshControl);
  const customRangeClassName = controlClassName(styles.customRangeControl);
  // Ant Design's inline Form.Item adds its own trailing margin. Space is the
  // single owner of compact-header spacing, so each visible control stays 12px
  // apart even when a sibling (such as Analysis refresh) lives outside Form.
  const compactItemStyle = compact ? { marginInlineEnd: 0 } : undefined;
  const canRenderAutoRefresh = showAutoRefresh && autoRefreshOptions && autoRefreshInterval !== undefined && onAutoRefreshChange;
  const rangePopoverContent = (
    <div className={styles.timeRangePopoverContent}>
      <section className={styles.timeRangeSection} aria-label={t('usage_stats.range_quick_ranges')}>
        <div className={styles.timeRangeSectionTitle}>{t('usage_stats.range_quick_ranges')}</div>
        <div className={styles.quickRangeGrid}>
          {presetTimeRangeOptions.map((option) => {
            const selected = timeRange === option.value;
            return (
              <Button
                key={option.value}
                type="text"
                className={styles.quickRangeOption}
                aria-pressed={selected}
                onClick={() => handlePresetRangeChange(option.value)}
              >
                <span>{option.label}</span>
                {selected && <CheckOutlined aria-hidden="true" />}
              </Button>
            );
          })}
        </div>
      </section>
      <section className={styles.timeRangeSection} aria-label={t('usage_stats.range_absolute')}>
        <div className={styles.timeRangeSectionTitle}>{t('usage_stats.range_absolute')}</div>
        <DatePicker.RangePicker
          className={customRangeClassName}
          value={draftCustomRangeValue}
          format={DATE_FORMAT}
          minDate={minimumDate}
          maxDate={maximumDate}
          disabled={disabled}
          onChange={handleDraftCustomRangeChange}
          aria-label={t('usage_stats.custom_range')}
          getPopupContainer={(trigger) => trigger.parentElement ?? document.body}
        />
        {(customRangeError || customRangeHint) && (
          <div className={customRangeError ? styles.timeRangeError : styles.timeRangeHint}>
            {customRangeError || customRangeHint}
          </div>
        )}
        <div className={styles.timeRangeActions}>
          <Button type="primary" disabled={!canApplyCustomRange || disabled} onClick={handleApplyCustomRange}>
            {t('usage_stats.range_apply')}
          </Button>
        </div>
      </section>
    </div>
  );

  return (
    <Form
      className={`${styles.usageFilterBar} ${compact ? styles.compact : ''} ${className ?? ''}`.trim()}
      layout={layout}
      requiredMark={false}
    >
      <Space
        orientation={isVertical ? 'vertical' : 'horizontal'}
        wrap={!isVertical}
        align={isVertical ? undefined : 'start'}
        size={isVertical ? 12 : [12, 12]}
        style={isVertical ? { width: '100%' } : undefined}
      >
        {canRenderAutoRefresh && (
          <Form.Item
            label={compact ? undefined : t('usage_stats.auto_refresh')}
            style={compactItemStyle}
          >
            <Select
              className={autoRefreshClassName}
              value={autoRefreshInterval}
              options={autoRefreshOptions}
              onChange={(value) => onAutoRefreshChange(value as number)}
              aria-label={t('usage_stats.auto_refresh')}
              disabled={disabled}
            />
          </Form.Item>
        )}
        {showApiKeyFilter && (
          <Form.Item
            label={compact ? undefined : t('usage_stats.api_key_filter')}
            style={compactItemStyle}
          >
            <Select
              className={apiKeyClassName}
              value={selectedApiKeyId}
              options={apiKeySelectOptions}
              onChange={onApiKeyChange}
              popupMatchSelectWidth={compact ? COMPACT_API_KEY_POPUP_WIDTH : undefined}
              aria-label={t('usage_stats.api_key_filter')}
              disabled={disabled}
            />
          </Form.Item>
        )}
        <Form.Item
          label={compact ? undefined : t('usage_stats.range_filter')}
          style={compactItemStyle}
        >
          <Popover
            content={rangePopoverContent}
            trigger="click"
            placement={isVertical ? 'bottomLeft' : 'bottomRight'}
            open={rangePopoverOpen}
            onOpenChange={handleRangePopoverOpenChange}
            destroyOnHidden={false}
            classNames={{ root: styles.timeRangePopover }}
          >
            <Button
              className={`${rangeClassName ?? ''} ${styles.timeRangeTrigger}`.trim()}
              icon={<ClockCircleOutlined />}
              disabled={disabled}
              aria-label={t('usage_stats.range_filter')}
              aria-expanded={rangePopoverOpen}
            >
              <span className={styles.timeRangeTriggerLabel}>{selectedRangeLabel}</span>
              <DownOutlined className={styles.timeRangeTriggerChevron} aria-hidden="true" />
            </Button>
          </Popover>
        </Form.Item>
      </Space>
    </Form>
  );
}
