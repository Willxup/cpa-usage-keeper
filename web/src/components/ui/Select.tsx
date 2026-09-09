import React, {
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type ReactNode
} from 'react';
import { createPortal } from 'react-dom';
import { IconChevronDown } from './icons';
import styles from './Select.module.scss';

export interface SelectOption {
  value: string;
  label: string;
  triggerLabel?: string;
  suffix?: ReactNode;
  suffixAriaLabel?: string;
  disabled?: boolean;
}

interface SelectProps {
  value: string;
  options: ReadonlyArray<SelectOption>;
  onChange: (value: string) => void;
  placeholder?: string;
  className?: string;
  dropdownClassName?: string;
  disabled?: boolean;
  ariaLabel?: string;
  ariaLabelledBy?: string;
  ariaDescribedBy?: string;
  fullWidth?: boolean;
  dropdownMinWidth?: number;
  id?: string;
  search?: {
    placeholder: string;
    noResultsText: string;
  };
}

const VIEWPORT_MARGIN = 8;
const DROPDOWN_OFFSET = 6;
const DROPDOWN_MAX_HEIGHT = 240;
const DROPDOWN_Z_INDEX = 2010;

const clamp = (value: number, min: number, max: number) => Math.min(Math.max(value, min), max);

const findNextEnabledOptionIndex = (
  options: ReadonlyArray<SelectOption>,
  startIndex: number,
  direction: 1 | -1,
) => {
  if (options.length === 0) return -1;
  for (let offset = 1; offset <= options.length; offset += 1) {
    const index = (startIndex + direction * offset + options.length) % options.length;
    if (!options[index]?.disabled) {
      return index;
    }
  }
  return -1;
};

const resolveDropdownStyle = (element: HTMLElement, dropdownMinWidth?: number): CSSProperties => {
  const rect = element.getBoundingClientRect();
  const viewportWidth = window.innerWidth;
  const viewportHeight = window.innerHeight;
  const availableWidth = Math.max(0, viewportWidth - VIEWPORT_MARGIN * 2);
  const width = Math.min(Math.max(rect.width, dropdownMinWidth ?? 0), availableWidth);
  const left = clamp(
    rect.left - (width - rect.width) / 2,
    VIEWPORT_MARGIN,
    Math.max(VIEWPORT_MARGIN, viewportWidth - width - VIEWPORT_MARGIN)
  );
  const spaceBelow = viewportHeight - rect.bottom - VIEWPORT_MARGIN - DROPDOWN_OFFSET;
  const spaceAbove = rect.top - VIEWPORT_MARGIN - DROPDOWN_OFFSET;
  const direction =
    spaceBelow >= DROPDOWN_MAX_HEIGHT || spaceBelow >= spaceAbove ? 'down' : 'up';
  const maxHeight = Math.max(
    0,
    Math.min(DROPDOWN_MAX_HEIGHT, direction === 'down' ? spaceBelow : spaceAbove)
  );

  return direction === 'down'
    ? {
        position: 'fixed',
        top: rect.bottom + DROPDOWN_OFFSET,
        left,
        width,
        maxHeight,
        zIndex: DROPDOWN_Z_INDEX
      }
    : {
        position: 'fixed',
        bottom: viewportHeight - rect.top + DROPDOWN_OFFSET,
        left,
        width,
        maxHeight,
        zIndex: DROPDOWN_Z_INDEX
      };
};

export function Select({
  value,
  options,
  onChange,
  placeholder,
  className,
  dropdownClassName,
  disabled = false,
  ariaLabel,
  ariaLabelledBy,
  ariaDescribedBy,
  fullWidth = true,
  dropdownMinWidth,
  id,
  search,
}: SelectProps) {
  const generatedId = useId();
  const selectId = id ?? generatedId;
  const listboxId = `${selectId}-listbox`;
  const [open, setOpen] = useState(false);
  const [highlightedIndex, setHighlightedIndex] = useState(-1);
  const [searchQuery, setSearchQuery] = useState('');
  const searchInputRef = useRef<HTMLInputElement | null>(null);
  const wrapRef = useRef<HTMLDivElement | null>(null);
  const dropdownRef = useRef<HTMLDivElement | null>(null);
  const rafRef = useRef<number | null>(null);
  const [dropdownStyle, setDropdownStyle] = useState<CSSProperties | null>(null);
  const isOpen = open && !disabled;
  const searchable = Boolean(search);
  // 搜索只缩小候选项，提交选择后才更新调用方的筛选值。
  const visibleOptions = useMemo(() => {
    const query = searchable ? searchQuery.trim().toLowerCase() : '';
    return query ? options.filter((option) => option.label.toLowerCase().includes(query)) : options;
  }, [options, searchable, searchQuery]);
  const openDropdown = useCallback(() => {
    setSearchQuery('');
    setHighlightedIndex(-1);
    setOpen(true);
  }, []);

  useEffect(() => {
    if (!open || disabled) return;
    const handleClickOutside = (event: MouseEvent) => {
      const target = event.target as Node;
      if (wrapRef.current?.contains(target) || dropdownRef.current?.contains(target)) return;
      setOpen(false);
    };
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, [disabled, open]);

  const updateDropdownStyle = useCallback(() => {
    if (!wrapRef.current) return;
    setDropdownStyle(resolveDropdownStyle(wrapRef.current, dropdownMinWidth));
  }, [dropdownMinWidth]);

  const scheduleDropdownStyleUpdate = useCallback(() => {
    if (typeof window === 'undefined') return;
    if (rafRef.current !== null) {
      window.cancelAnimationFrame(rafRef.current);
    }
    rafRef.current = window.requestAnimationFrame(() => {
      rafRef.current = null;
      updateDropdownStyle();
    });
  }, [updateDropdownStyle]);

  useLayoutEffect(() => {
    if (!isOpen) {
      if (rafRef.current !== null && typeof window !== 'undefined') {
        window.cancelAnimationFrame(rafRef.current);
        rafRef.current = null;
      }
      return;
    }

    updateDropdownStyle();

    const handleViewportChange = () => {
      scheduleDropdownStyleUpdate();
    };

    const resizeObserver =
      typeof ResizeObserver !== 'undefined' && wrapRef.current
        ? new ResizeObserver(() => {
            scheduleDropdownStyleUpdate();
          })
        : null;

    if (resizeObserver && wrapRef.current) {
      resizeObserver.observe(wrapRef.current);
    }

    window.addEventListener('resize', handleViewportChange);
    window.addEventListener('scroll', handleViewportChange, true);

    return () => {
      window.removeEventListener('resize', handleViewportChange);
      window.removeEventListener('scroll', handleViewportChange, true);
      resizeObserver?.disconnect();
      if (rafRef.current !== null) {
        window.cancelAnimationFrame(rafRef.current);
        rafRef.current = null;
      }
    };
  }, [isOpen, scheduleDropdownStyleUpdate, updateDropdownStyle]);

  const selectedIndex = useMemo(() => options.findIndex((option) => option.value === value), [options, value]);
  const visibleSelectedIndex = visibleOptions.findIndex((option) => option.value === value);
  const firstEnabledIndex = visibleOptions.findIndex((option) => !option.disabled);
  const resolvedHighlightedIndex =
    highlightedIndex >= 0 && visibleOptions[highlightedIndex] && !visibleOptions[highlightedIndex].disabled
      ? highlightedIndex
      : visibleSelectedIndex >= 0 && !visibleOptions[visibleSelectedIndex]?.disabled
        ? visibleSelectedIndex
        : firstEnabledIndex;
  const selected = selectedIndex >= 0 ? options[selectedIndex] : undefined;
  const displayText = selected?.triggerLabel ?? selected?.label ?? placeholder ?? '';
  const isPlaceholder = !selected && placeholder;

  const commitSelection = useCallback(
    (nextIndex: number) => {
      const nextOption = visibleOptions[nextIndex];
      if (!nextOption || nextOption.disabled) return;
      // 先保留输入焦点再关闭，避免 onFocus 在选中后重新展开列表。
      if (searchable) searchInputRef.current?.focus();
      onChange(nextOption.value);
      setOpen(false);
      setHighlightedIndex(nextIndex);
    },
    [onChange, searchable, visibleOptions]
  );

  const moveHighlight = useCallback(
    (direction: 1 | -1) => {
      if (visibleOptions.length === 0) return;
      const startIndex = resolvedHighlightedIndex >= 0
        ? resolvedHighlightedIndex
        : direction === 1
          ? -1
          : visibleOptions.length;
      const nextIndex = findNextEnabledOptionIndex(visibleOptions, startIndex, direction);
      if (nextIndex < 0) return;
      setHighlightedIndex(nextIndex);
    },
    [visibleOptions, resolvedHighlightedIndex]
  );

  const handleKeyDown = useCallback(
    (event: React.KeyboardEvent<HTMLButtonElement | HTMLInputElement>) => {
      if (disabled || event.nativeEvent.isComposing) return;
      const editingSearch = event.currentTarget instanceof HTMLInputElement;

      switch (event.key) {
        case 'ArrowDown':
          event.preventDefault();
          if (!isOpen) {
            openDropdown();
            return;
          }
          moveHighlight(1);
          return;
        case 'ArrowUp':
          event.preventDefault();
          if (!isOpen) {
            openDropdown();
            return;
          }
          moveHighlight(-1);
          return;
        case 'Home':
          if (editingSearch || !isOpen || visibleOptions.length === 0) return;
          event.preventDefault();
          setHighlightedIndex(0);
          return;
        case 'End':
          if (editingSearch || !isOpen || visibleOptions.length === 0) return;
          event.preventDefault();
          setHighlightedIndex(visibleOptions.length - 1);
          return;
        case 'Enter':
        case ' ': {
          if (editingSearch && event.key === ' ') return;
          event.preventDefault();
          if (!isOpen) {
            openDropdown();
            return;
          }
          if (resolvedHighlightedIndex >= 0) {
            commitSelection(resolvedHighlightedIndex);
          }
          return;
        }
        case 'Escape':
          if (!isOpen) return;
          event.preventDefault();
          if (searchable) searchInputRef.current?.focus();
          setOpen(false);
          return;
        case 'Tab':
          if (isOpen) setOpen(false);
          return;
        default:
          return;
      }
    },
    [commitSelection, disabled, isOpen, moveHighlight, openDropdown, searchable, visibleOptions.length, resolvedHighlightedIndex]
  );

  useEffect(() => {
    if (!isOpen || resolvedHighlightedIndex < 0) return;
    const highlightedOption = document.getElementById(`${selectId}-option-${resolvedHighlightedIndex}`);
    highlightedOption?.scrollIntoView({ block: 'nearest' });
  }, [isOpen, resolvedHighlightedIndex, selectId, visibleOptions]);

  const optionButtons = isOpen && visibleOptions.map((opt, index) => {
    const active = opt.value === value;
    const highlighted = index === resolvedHighlightedIndex;
    return (
      <button
        key={opt.value}
        id={`${selectId}-option-${index}`}
        type="button"
        role="option"
        aria-selected={active}
        aria-disabled={opt.disabled || undefined}
        className={`${styles.option} ${active ? styles.optionActive : ''} ${highlighted ? styles.optionHighlighted : ''} ${opt.disabled ? styles.optionDisabled : ''}`.trim()}
        disabled={opt.disabled}
        tabIndex={searchable ? -1 : undefined}
        onMouseDown={searchable ? (event) => event.preventDefault() : undefined}
        onMouseEnter={opt.disabled ? undefined : () => setHighlightedIndex(index)}
        onKeyDown={handleKeyDown}
        onClick={opt.disabled ? undefined : () => commitSelection(index)}
      >
        <span className={styles.optionLabel}>{opt.label}</span>
        {opt.suffix ? (
          <span className={styles.optionSuffix} aria-label={opt.suffixAriaLabel}>
            {opt.suffix}
          </span>
        ) : null}
      </button>
    );
  });

  const dropdown =
    isOpen && dropdownStyle
      ? (
          <div
            ref={dropdownRef}
            className={`${styles.dropdown} ${searchable ? styles.searchableDropdown : ''} ${dropdownClassName ?? ''}`.trim()}
            id={searchable ? undefined : listboxId}
            role={searchable ? undefined : 'listbox'}
            aria-label={searchable ? undefined : ariaLabel}
            style={dropdownStyle}
          >
            {search ? (
              <>
                <div id={listboxId} role="listbox" aria-label={ariaLabel} className={styles.searchOptions}>
                  {optionButtons}
                </div>
                {visibleOptions.length === 0 ? <div role="status" className={styles.noResults}>{search.noResultsText}</div> : null}
              </>
            ) : optionButtons}
          </div>
        )
      : null;

  return (
    <>
      <div
        className={`${styles.wrap} ${fullWidth ? styles.wrapFullWidth : ''} ${className ?? ''}`}
        ref={wrapRef}
      >
        {search ? (
          <>
            <input
              ref={searchInputRef}
              id={selectId}
              className={`${styles.trigger} ${styles.searchInput}`}
              type="text"
              role="combobox"
              aria-label={ariaLabel ?? search.placeholder}
              aria-labelledby={ariaLabelledBy}
              aria-describedby={ariaDescribedBy}
              aria-autocomplete="list"
              aria-expanded={isOpen}
              aria-controls={isOpen ? listboxId : undefined}
              aria-activedescendant={isOpen && resolvedHighlightedIndex >= 0 ? `${selectId}-option-${resolvedHighlightedIndex}` : undefined}
              placeholder={search.placeholder}
              value={isOpen ? searchQuery : selected?.triggerLabel ?? selected?.label ?? ''}
              autoComplete="off"
              autoCapitalize="none"
              spellCheck={false}
              disabled={disabled}
              onFocus={openDropdown}
              onClick={() => {
                if (!isOpen) openDropdown();
              }}
              onChange={(event) => {
                setSearchQuery(event.target.value);
                setHighlightedIndex(-1);
                setOpen(true);
              }}
              onBlur={(event) => {
                if (!dropdownRef.current?.contains(event.relatedTarget)) setOpen(false);
              }}
              onKeyDown={handleKeyDown}
            />
            <span className={`${styles.triggerIcon} ${styles.searchIcon}`} aria-hidden="true">
              <IconChevronDown size={14} />
            </span>
          </>
        ) : <button
          id={selectId}
          type="button"
          className={styles.trigger}
          onClick={disabled ? undefined : () => isOpen ? setOpen(false) : openDropdown()}
          onKeyDown={handleKeyDown}
          aria-haspopup="listbox"
          aria-expanded={isOpen}
          aria-controls={isOpen ? listboxId : undefined}
          aria-activedescendant={
            isOpen && resolvedHighlightedIndex >= 0
              ? `${selectId}-option-${resolvedHighlightedIndex}`
              : undefined
          }
          aria-label={ariaLabel}
          aria-labelledby={ariaLabelledBy}
          aria-describedby={ariaDescribedBy}
          disabled={disabled}
        >
          <span className={`${styles.triggerText} ${isPlaceholder ? styles.placeholder : ''}`}>
            {displayText}
          </span>
          <span className={styles.triggerIcon} aria-hidden="true">
            <IconChevronDown size={14} />
          </span>
        </button>}
      </div>
      {dropdown && (typeof document === 'undefined' ? dropdown : createPortal(dropdown, document.body))}
    </>
  );
}
