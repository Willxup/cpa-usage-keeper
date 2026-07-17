import { useCallback, useEffect, useRef, useState, type CSSProperties, type KeyboardEvent } from 'react';
import { createPortal } from 'react-dom';
import { useTranslation } from 'react-i18next';
import type { UsageCustomRange, UsageTimeRange } from '@/lib/types';
import { Modal } from '@/components/ui/Modal';
import { IconChevronDown, IconTimer } from '@/components/ui/icons';
import { formatCustomRangeLabel, normalizeCustomRange } from '@/utils/usage/customRange';
import {
  buildRollingUsageRange,
  parseSelectableUsageRange,
  type UsageTimeRangeMode,
} from '@/utils/usage/rangeQuery';
import { CustomRangePanel } from './CustomRangePanel';
import styles from './TimeRangeControl.module.scss';

type RollingUnit = Extract<UsageTimeRangeMode, 'hour' | 'day'>;

const MODE_OPTIONS: ReadonlyArray<{ value: UsageTimeRangeMode; labelKey: string }> = [
  { value: 'hour', labelKey: 'usage_stats.range_unit_hour' },
  { value: 'day', labelKey: 'usage_stats.range_unit_day' },
  { value: 'today', labelKey: 'usage_stats.range_today' },
  { value: 'yesterday', labelKey: 'usage_stats.range_yesterday' },
  { value: 'custom', labelKey: 'usage_stats.range_custom' },
];

const DEFAULT_ROLLING_VALUES: Record<RollingUnit, number> = { hour: 8, day: 7 };
const MOBILE_BREAKPOINT_PX = 768;
const RANGE_DIALOG_FOCUSABLE_SELECTOR = 'button:not([disabled]), input:not([disabled]), [tabindex]:not([tabindex="-1"])';

const getRollingMinimum = (unit: RollingUnit) => unit === 'hour' ? 5 : 1;

const getRollingMaximum = (unit: RollingUnit) => unit === 'hour' ? 24 : 30;

const getRollingTicks = (unit: RollingUnit) => unit === 'hour'
  ? [5, 8, 12, 18, 24]
  : [1, 7, 14, 21, 30];

type LiquidParticleMotion = 'a' | 'b' | 'c';

interface LiquidParticle {
  x: number;
  y: number;
  size: number;
  duration: number;
  delay: number;
  color: string;
  motion: LiquidParticleMotion;
}

const LIQUID_PARTICLES: ReadonlyArray<LiquidParticle> = [
  { x: 6, y: 66, size: 1.8, duration: 7.2, delay: -1.6, color: 'rgba(208, 235, 255, 0.84)', motion: 'a' },
  { x: 11, y: 34, size: 2.2, duration: 8.6, delay: -5.1, color: 'rgba(236, 244, 255, 0.78)', motion: 'b' },
  { x: 17, y: 74, size: 1.5, duration: 6.4, delay: -3.8, color: 'rgba(255, 230, 252, 0.82)', motion: 'c' },
  { x: 23, y: 47, size: 2.6, duration: 9.1, delay: -7.2, color: 'rgba(223, 232, 255, 0.78)', motion: 'a' },
  { x: 29, y: 27, size: 1.7, duration: 5.9, delay: -2.4, color: 'rgba(244, 218, 255, 0.86)', motion: 'b' },
  { x: 35, y: 68, size: 2.1, duration: 7.8, delay: -6.3, color: 'rgba(210, 239, 255, 0.80)', motion: 'c' },
  { x: 41, y: 42, size: 1.6, duration: 8.3, delay: -4.7, color: 'rgba(255, 239, 255, 0.88)', motion: 'a' },
  { x: 47, y: 76, size: 2.7, duration: 6.8, delay: -1.9, color: 'rgba(246, 207, 255, 0.78)', motion: 'b' },
  { x: 52, y: 30, size: 1.8, duration: 9.6, delay: -8.1, color: 'rgba(220, 234, 255, 0.82)', motion: 'c' },
  { x: 58, y: 58, size: 2.3, duration: 7.5, delay: -5.6, color: 'rgba(255, 232, 252, 0.86)', motion: 'a' },
  { x: 63, y: 22, size: 1.5, duration: 6.1, delay: -3.3, color: 'rgba(213, 242, 255, 0.82)', motion: 'b' },
  { x: 68, y: 70, size: 2.5, duration: 8.9, delay: -7.6, color: 'rgba(250, 214, 255, 0.80)', motion: 'c' },
  { x: 73, y: 39, size: 1.9, duration: 7.0, delay: -2.8, color: 'rgba(232, 240, 255, 0.86)', motion: 'a' },
  { x: 78, y: 78, size: 1.6, duration: 9.4, delay: -6.9, color: 'rgba(255, 229, 250, 0.78)', motion: 'b' },
  { x: 83, y: 28, size: 2.4, duration: 6.6, delay: -4.2, color: 'rgba(207, 235, 255, 0.84)', motion: 'c' },
  { x: 87, y: 62, size: 1.7, duration: 8.1, delay: -1.3, color: 'rgba(248, 219, 255, 0.84)', motion: 'a' },
  { x: 91, y: 43, size: 2.1, duration: 7.7, delay: -5.9, color: 'rgba(228, 241, 255, 0.82)', motion: 'b' },
  { x: 95, y: 72, size: 1.5, duration: 9.8, delay: -8.7, color: 'rgba(255, 235, 254, 0.86)', motion: 'c' },
];

const LIQUID_PARTICLE_MOTION_CLASSES: Record<LiquidParticleMotion, string> = {
  a: styles.liquidParticleA,
  b: styles.liquidParticleB,
  c: styles.liquidParticleC,
};

interface RangePanelProps {
  mode: UsageTimeRangeMode;
  rollingValues: Record<RollingUnit, number>;
  customRange: UsageCustomRange;
  customAnchorMs: number;
  timeZone: string;
  customEnabled: boolean;
  locale?: string;
  onModeChange: (mode: UsageTimeRangeMode) => void;
  onRollingValueChange: (unit: RollingUnit, value: number) => void;
  onRollingPointerStart: (unit: RollingUnit, value: number, pointerId: number) => boolean;
  onRollingValueCommit: (unit: RollingUnit, value: number, pointerId?: number) => void;
  onCustomRangeChange: (value: UsageCustomRange) => void;
  onCustomApply: () => void;
  onCustomCancel: () => void;
}

function TimeRangePanel({
  mode,
  rollingValues,
  customRange,
  customAnchorMs,
  timeZone,
  customEnabled,
  locale,
  onModeChange,
  onRollingValueChange,
  onRollingPointerStart,
  onRollingValueCommit,
  onCustomRangeChange,
  onCustomApply,
  onCustomCancel,
}: RangePanelProps) {
  const { t } = useTranslation();
  const isRolling = mode === 'hour' || mode === 'day';
  const rollingUnit: RollingUnit = mode === 'day' ? 'day' : 'hour';
  const rollingValue = rollingValues[rollingUnit];
  const minimum = getRollingMinimum(rollingUnit);
  const maximum = getRollingMaximum(rollingUnit);
  const progress = ((rollingValue - minimum) / (maximum - minimum)) * 100;
  const sliderStyle = { '--range-progress': `${progress}%` } as CSSProperties;
  const ticks = getRollingTicks(rollingUnit);

  const commitKeyboardValue = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key.startsWith('Arrow') || event.key === 'Home' || event.key === 'End' || event.key === 'PageUp' || event.key === 'PageDown') {
      onRollingValueCommit(rollingUnit, Number(event.currentTarget.value));
    }
  };

  return (
    <div className={styles.rangePanel}>
      <div className={styles.modeSelector} role="group" aria-label={t('usage_stats.range_filter')}>
        {MODE_OPTIONS.map((option) => (
          <button
            key={option.value}
            type="button"
            className={mode === option.value ? styles.modeButtonActive : styles.modeButton}
            data-time-range-mode={option.value}
            aria-pressed={mode === option.value}
            disabled={option.value === 'custom' && !customEnabled}
            onClick={() => onModeChange(option.value)}
          >
            {t(option.labelKey)}
          </button>
        ))}
      </div>

      {isRolling ? (
        <div className={styles.sliderSection}>
          <div className={styles.sliderHeader}>
            <span>{t('usage_stats.range_recent_window')}</span>
            <strong>
              {rollingValue}
              <small>{t(rollingUnit === 'hour' ? 'usage_stats.range_value_hour' : 'usage_stats.range_value_day', { count: rollingValue })}</small>
            </strong>
          </div>
          <div className={styles.sliderControl} style={sliderStyle}>
            <div className={styles.sliderRail} aria-hidden="true">
              <span className={styles.sliderFill}>
                {LIQUID_PARTICLES.map((particle, index) => (
                  <span
                    key={index}
                    className={`${styles.liquidParticle} ${LIQUID_PARTICLE_MOTION_CLASSES[particle.motion]}`}
                    data-liquid-particle
                    data-particle-motion={particle.motion}
                    style={{
                      '--liquid-particle-x': `${particle.x}%`,
                      '--liquid-particle-y': `${particle.y}%`,
                      '--liquid-particle-size': `${particle.size}px`,
                      '--liquid-particle-duration': `${particle.duration}s`,
                      '--liquid-particle-delay': `${particle.delay}s`,
                      '--liquid-particle-color': particle.color,
                    } as CSSProperties}
                  />
                ))}
              </span>
              {ticks.map((tick) => {
                const position = ((tick - minimum) / (maximum - minimum)) * 100;
                return (
                  <span
                    key={tick}
                    className={tick <= rollingValue ? styles.sliderDotActive : styles.sliderDot}
                    style={{ '--range-dot-position': `${position}%` } as CSSProperties}
                  />
                );
              })}
            </div>
            <input
              className={styles.rangeInput}
              data-time-range-slider
              type="range"
              min={minimum}
              max={maximum}
              step={1}
              value={rollingValue}
              aria-label={t(
                rollingUnit === 'hour' ? 'usage_stats.range_last_hours' : 'usage_stats.range_last_days',
                { count: rollingValue },
              )}
              onPointerDown={(event) => {
                if (!onRollingPointerStart(rollingUnit, Number(event.currentTarget.value), event.pointerId)) {
                  event.preventDefault();
                }
              }}
              onInput={(event) => onRollingValueChange(rollingUnit, Number(event.currentTarget.value))}
              onPointerUp={(event) => onRollingValueCommit(rollingUnit, Number(event.currentTarget.value), event.pointerId)}
              onKeyUp={commitKeyboardValue}
              onBlur={(event) => onRollingValueCommit(rollingUnit, Number(event.currentTarget.value))}
            />
          </div>
          <div className={styles.rangeTicks} aria-hidden="true">
            {ticks.map((tick) => <span key={tick}>{tick}</span>)}
          </div>
        </div>
      ) : mode === 'custom' ? (
        <CustomRangePanel
          value={customRange}
          timeZone={timeZone}
          locale={locale}
          anchorMs={customAnchorMs}
          onChange={onCustomRangeChange}
          onApply={onCustomApply}
          onCancel={onCustomCancel}
        />
      ) : (
        <div className={styles.naturalDaySummary} data-time-range-natural-summary={mode}>
          <span className={styles.naturalDayIcon} aria-hidden="true">◷</span>
          <span>
            <strong>{t(mode === 'today' ? 'usage_stats.range_today' : 'usage_stats.range_yesterday')}</strong>
            <small>{t(mode === 'today' ? 'usage_stats.range_today_bounds' : 'usage_stats.range_yesterday_bounds')}</small>
          </span>
        </div>
      )}
    </div>
  );
}

interface TimeRangeControlProps {
  value: UsageTimeRange;
  customRange?: UsageCustomRange;
  onChange: (value: UsageTimeRange, customRange?: UsageCustomRange) => void;
  ariaLabel: string;
  timeZone?: string;
}

export function TimeRangeControl({ value, customRange, onChange, ariaLabel, timeZone: providedTimeZone }: TimeRangeControlProps) {
  const { t, i18n } = useTranslation();
  const timeZone = providedTimeZone?.trim() || 'UTC';
  const customEnabled = Boolean(providedTimeZone?.trim());
  const parsedRange = parseSelectableUsageRange(value);
  const appliedMode: UsageTimeRangeMode = value === 'custom' ? 'custom' : parsedRange.mode;
  const [pendingMode, setPendingMode] = useState<UsageTimeRangeMode | null>(null);
  const panelMode = pendingMode ?? appliedMode;
  const [customAnchorMs, setCustomAnchorMs] = useState(() => Date.now());
  const [customDraft, setCustomDraft] = useState<UsageCustomRange>(() => normalizeCustomRange(customRange, {
    nowMs: customAnchorMs,
    timeZone,
  }));
  const [rollingValues, setRollingValues] = useState<Record<RollingUnit, number>>(() => ({
    ...DEFAULT_ROLLING_VALUES,
    ...(appliedMode === 'hour' || appliedMode === 'day' ? { [appliedMode]: parsedRange.value ?? DEFAULT_ROLLING_VALUES[appliedMode] } : {}),
  }));
  const [draftingUnit, setDraftingUnit] = useState<RollingUnit | null>(null);
  const [desktopOpen, setDesktopOpen] = useState(false);
  const [mobileOpen, setMobileOpen] = useState(false);
  const [popoverPosition, setPopoverPosition] = useState({ top: 0, left: 0 });
  const [previousAppliedMode, setPreviousAppliedMode] = useState(appliedMode);
  const desktopTriggerRef = useRef<HTMLButtonElement | null>(null);
  const desktopPopoverRef = useRef<HTMLDivElement | null>(null);
  const mobileOpenRef = useRef(false);
  const lastEmittedRangeRef = useRef<UsageTimeRange | null>(null);
  const latestRollingValuesRef = useRef(rollingValues);
  const activeRollingPointerRef = useRef<{ unit: RollingUnit; pointerId: number } | null>(null);

  if (previousAppliedMode !== appliedMode) {
    setPreviousAppliedMode(appliedMode);
    if (appliedMode === 'custom' && previousAppliedMode !== 'custom') {
      setCustomDraft(normalizeCustomRange(customRange, { nowMs: customAnchorMs, timeZone }));
    }
  }

  const displayedRollingValues: Record<RollingUnit, number> = {
    ...rollingValues,
    ...(appliedMode === 'hour' || appliedMode === 'day') && draftingUnit !== appliedMode
      ? { [appliedMode]: parsedRange.value ?? DEFAULT_ROLLING_VALUES[appliedMode] }
      : {},
  };

  useEffect(() => {
    if (lastEmittedRangeRef.current === value) {
      lastEmittedRangeRef.current = null;
    }
  }, [value]);

  const formatCurrentLabel = () => {
    if (appliedMode === 'today') return t('usage_stats.range_today');
    if (appliedMode === 'yesterday') return t('usage_stats.range_yesterday');
    if (appliedMode === 'custom') {
      return customRange
        ? formatCustomRangeLabel(customRange, { locale: i18n?.language, timeZone })
        : t('usage_stats.range_custom');
    }
    return t(appliedMode === 'hour' ? 'usage_stats.range_last_hours' : 'usage_stats.range_last_days', {
      count: displayedRollingValues[appliedMode],
    });
  };

  const prepareCustomDraft = useCallback(() => {
    const anchorMs = Date.now();
    setCustomAnchorMs(anchorMs);
    setCustomDraft(normalizeCustomRange(customRange, { nowMs: anchorMs, timeZone }));
  }, [customRange, timeZone]);

  const handleModeChange = (nextMode: UsageTimeRangeMode) => {
    if (appliedMode === 'hour' || appliedMode === 'day') {
      setRollingValues((current) => ({ ...current, [appliedMode]: displayedRollingValues[appliedMode] }));
    }
    setDraftingUnit(null);
    if (nextMode === 'custom') {
      if (!customEnabled) return;
      prepareCustomDraft();
      setPendingMode('custom');
      return;
    }
    setPendingMode(null);
    if (nextMode === 'today' || nextMode === 'yesterday') {
      onChange(nextMode);
      return;
    }
    onChange(buildRollingUsageRange(nextMode, displayedRollingValues[nextMode]));
  };

  const handleRollingValueChange = (unit: RollingUnit, nextValue: number) => {
    latestRollingValuesRef.current = { ...latestRollingValuesRef.current, [unit]: nextValue };
    setDraftingUnit(unit);
    setRollingValues((current) => ({ ...current, [unit]: nextValue }));
  };

  const handleRollingPointerStart = (unit: RollingUnit, currentValue: number, pointerId: number) => {
    const activePointer = activeRollingPointerRef.current;
    if (activePointer && activePointer.pointerId !== pointerId) return false;
    activeRollingPointerRef.current = { unit, pointerId };
    latestRollingValuesRef.current = { ...latestRollingValuesRef.current, [unit]: currentValue };
    return true;
  };

  const handleRollingValueCommit = useCallback((unit: RollingUnit, committedValue: number, pointerId?: number) => {
    const activePointer = activeRollingPointerRef.current;
    if (pointerId !== undefined && (!activePointer || activePointer.pointerId !== pointerId)) return;
    activeRollingPointerRef.current = null;
    setDraftingUnit((current) => current === unit ? null : current);
    const nextRange = buildRollingUsageRange(unit, committedValue);
    if (nextRange === value || nextRange === lastEmittedRangeRef.current) return;
    lastEmittedRangeRef.current = nextRange;
    onChange(nextRange);
  }, [onChange, value]);

  useEffect(() => {
    const finishActivePointer = (event: PointerEvent) => {
      const activePointer = activeRollingPointerRef.current;
      if (!activePointer || activePointer.pointerId !== event.pointerId) return;
      handleRollingValueCommit(
        activePointer.unit,
        latestRollingValuesRef.current[activePointer.unit],
        event.pointerId,
      );
    };
    window.addEventListener('pointerup', finishActivePointer);
    window.addEventListener('pointercancel', finishActivePointer);
    return () => {
      window.removeEventListener('pointerup', finishActivePointer);
      window.removeEventListener('pointercancel', finishActivePointer);
    };
  }, [handleRollingValueCommit]);

  const updatePopoverPosition = useCallback(() => {
    const trigger = desktopTriggerRef.current;
    if (!trigger) return;
    const rect = trigger.getBoundingClientRect();
    const width = Math.min(368, window.innerWidth - 24);
    const left = Math.min(Math.max(12, rect.right - width), window.innerWidth - width - 12);
    setPopoverPosition({ top: rect.bottom + 8, left });
  }, []);

  const discardDraft = useCallback(() => {
    activeRollingPointerRef.current = null;
    setDraftingUnit(null);
    setPendingMode(null);
    if (appliedMode === 'custom') {
      setCustomDraft(normalizeCustomRange(customRange, { nowMs: customAnchorMs, timeZone }));
      return;
    }
    if (appliedMode !== 'hour' && appliedMode !== 'day') return;
    const appliedValue = parsedRange.value ?? DEFAULT_ROLLING_VALUES[appliedMode];
    latestRollingValuesRef.current = { ...latestRollingValuesRef.current, [appliedMode]: appliedValue };
    setRollingValues((current) => current[appliedMode] === appliedValue ? current : { ...current, [appliedMode]: appliedValue });
  }, [appliedMode, customAnchorMs, customRange, parsedRange.value, timeZone]);

  const closeDesktopPopover = useCallback((restoreFocus = false) => {
    discardDraft();
    setDesktopOpen(false);
    if (restoreFocus) {
      queueMicrotask(() => desktopTriggerRef.current?.focus());
    }
  }, [discardDraft]);

  const closeMobileModal = useCallback(() => {
    mobileOpenRef.current = false;
    discardDraft();
    setMobileOpen(false);
  }, [discardDraft]);

  const toggleDesktopPopover = useCallback(() => {
    mobileOpenRef.current = false;
    setMobileOpen(false);
    if (desktopOpen) {
      closeDesktopPopover();
      return;
    }
    if (appliedMode === 'custom') prepareCustomDraft();
    updatePopoverPosition();
    setDesktopOpen(true);
  }, [appliedMode, closeDesktopPopover, desktopOpen, prepareCustomDraft, updatePopoverPosition]);

  const openMobileModal = useCallback(() => {
    closeDesktopPopover();
    if (appliedMode === 'custom') prepareCustomDraft();
    mobileOpenRef.current = true;
    setMobileOpen(true);
  }, [appliedMode, closeDesktopPopover, prepareCustomDraft]);

  useEffect(() => {
    const handleViewportResize = () => {
      if (window.innerWidth <= MOBILE_BREAKPOINT_PX) {
        closeDesktopPopover();
        return;
      }
      if (mobileOpenRef.current && !desktopOpen) closeMobileModal();
    };
    window.addEventListener('resize', handleViewportResize);
    return () => window.removeEventListener('resize', handleViewportResize);
  }, [closeDesktopPopover, closeMobileModal, desktopOpen]);

  useEffect(() => {
    if (!desktopOpen) return;
    queueMicrotask(() => {
      const popover = desktopPopoverRef.current;
      const activeMode = popover?.querySelector<HTMLElement>('[data-time-range-mode][aria-pressed="true"]');
      const firstFocusable = popover?.querySelector<HTMLElement>(RANGE_DIALOG_FOCUSABLE_SELECTOR);
      (activeMode ?? firstFocusable ?? popover)?.focus();
    });
    const handlePointerDown = (event: PointerEvent) => {
      const target = event.target as Node;
      if (desktopTriggerRef.current?.contains(target) || desktopPopoverRef.current?.contains(target)) return;
      closeDesktopPopover();
    };
    const handleKeyDown = (event: globalThis.KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault();
        closeDesktopPopover(true);
        return;
      }
      if (event.key !== 'Tab') return;

      const popover = desktopPopoverRef.current;
      if (!popover) return;
      const focusableElements = Array.from(popover.querySelectorAll<HTMLElement>(RANGE_DIALOG_FOCUSABLE_SELECTOR));
      if (focusableElements.length === 0) {
        event.preventDefault();
        popover.focus();
        return;
      }
      const firstElement = focusableElements[0];
      const lastElement = focusableElements[focusableElements.length - 1];
      const activeElement = document.activeElement;
      if (!popover.contains(activeElement)) {
        event.preventDefault();
        firstElement.focus();
        return;
      }
      if (event.shiftKey && activeElement === firstElement) {
        event.preventDefault();
        lastElement.focus();
        return;
      }
      if (!event.shiftKey && activeElement === lastElement) {
        event.preventDefault();
        firstElement.focus();
      }
    };
    window.addEventListener('resize', updatePopoverPosition);
    window.addEventListener('scroll', updatePopoverPosition, true);
    document.addEventListener('pointerdown', handlePointerDown);
    document.addEventListener('keydown', handleKeyDown);
    return () => {
      window.removeEventListener('resize', updatePopoverPosition);
      window.removeEventListener('scroll', updatePopoverPosition, true);
      document.removeEventListener('pointerdown', handlePointerDown);
      document.removeEventListener('keydown', handleKeyDown);
    };
  }, [closeDesktopPopover, desktopOpen, updatePopoverPosition]);

  const handleCustomApply = () => {
    lastEmittedRangeRef.current = 'custom';
    mobileOpenRef.current = false;
    setPendingMode(null);
    setDesktopOpen(false);
    setMobileOpen(false);
    onChange('custom', customDraft);
  };

  const handleCustomCancel = () => {
    if (mobileOpen) {
      closeMobileModal();
      return;
    }
    closeDesktopPopover(true);
  };

  const panel = (
    <TimeRangePanel
      mode={panelMode}
      rollingValues={displayedRollingValues}
      customRange={customDraft}
      customAnchorMs={customAnchorMs}
      timeZone={timeZone}
      customEnabled={customEnabled}
      locale={i18n?.language}
      onModeChange={handleModeChange}
      onRollingValueChange={handleRollingValueChange}
      onRollingPointerStart={handleRollingPointerStart}
      onRollingValueCommit={handleRollingValueCommit}
      onCustomRangeChange={setCustomDraft}
      onCustomApply={handleCustomApply}
      onCustomCancel={handleCustomCancel}
    />
  );
  const currentLabel = formatCurrentLabel();

  return (
    <div className={styles.controlRoot}>
      <div className={styles.desktopShell} data-time-range-shell="desktop">
        <span className={styles.shellLabel}>{ariaLabel}</span>
        <button
          ref={desktopTriggerRef}
          type="button"
          className={styles.desktopTrigger}
          data-time-range-trigger="desktop"
          aria-label={`${ariaLabel}: ${currentLabel}`}
          aria-haspopup="dialog"
          aria-expanded={desktopOpen}
          onClick={toggleDesktopPopover}
        >
          <IconTimer size={16} className={styles.triggerIcon} />
          <span className={styles.triggerLabel}>{currentLabel}</span>
          <IconChevronDown size={14} className={styles.triggerChevron} />
        </button>
      </div>

      <div className={styles.mobileShell} data-time-range-shell="mobile">
        <span className={styles.shellLabel}>{ariaLabel}</span>
        <button
          type="button"
          className={styles.mobileTrigger}
          data-time-range-trigger="mobile"
          aria-label={`${ariaLabel}: ${currentLabel}`}
          aria-haspopup="dialog"
          aria-expanded={mobileOpen}
          onClick={openMobileModal}
        >
          <IconTimer size={16} className={styles.triggerIcon} />
          <span className={styles.triggerLabel}>{currentLabel}</span>
          <IconChevronDown size={16} className={styles.triggerChevron} />
        </button>
      </div>

      {desktopOpen && typeof document !== 'undefined' && createPortal(
        <div
          ref={desktopPopoverRef}
          className={styles.desktopPopover}
          style={{ top: popoverPosition.top, left: popoverPosition.left }}
          role="dialog"
          aria-label={ariaLabel}
          tabIndex={-1}
        >
          <div className={styles.popoverHeader}>
            <span>{ariaLabel}</span>
            <strong>{currentLabel}</strong>
          </div>
          {panel}
        </div>,
        document.body,
      )}

      <Modal
        open={mobileOpen}
        onClose={closeMobileModal}
        title={ariaLabel}
        width="min(430px, calc(100% - 24px))"
        className={styles.mobileRangeModal}
      >
        <div className={styles.mobileCurrentRange}>{currentLabel}</div>
        {panel}
      </Modal>
    </div>
  );
}
