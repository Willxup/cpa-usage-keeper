import React, {
  useCallback,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type ReactNode
} from 'react';
import { createPortal } from 'react-dom';
import { useAnchorPosition } from '@/hooks/useAnchorPosition';
import { useMediaQuery } from '@/hooks/useMediaQuery';
import { IconChevronDown } from './icons';
import { MenuScrollArea } from './MenuScrollArea';
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
  renderValue?: (option: SelectOption | undefined) => ReactNode;
  showChevron?: boolean;
  id?: string;
  search?: {
    placeholder: string;
    noResultsText: string;
  };
}

const VIEWPORT_MARGIN = 8;
const DROPDOWN_OFFSET = 8;
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

const resolveDropdownStyle = (rect: DOMRect, dropdownMinWidth?: number, touch = false): CSSProperties => {
  const viewport = window.visualViewport;
  const viewportWidth = viewport?.width ?? window.innerWidth;
  const viewportHeight = viewport?.height ?? window.innerHeight;
  const viewportLeft = viewport?.offsetLeft ?? 0;
  const viewportTop = viewport?.offsetTop ?? 0;
  const availableWidth = Math.max(0, viewportWidth - VIEWPORT_MARGIN * 2);
  const width = Math.min(Math.max(rect.width, dropdownMinWidth ?? 0), availableWidth);
  const left = clamp(
    rect.left,
    viewportLeft + VIEWPORT_MARGIN,
    Math.max(viewportLeft + VIEWPORT_MARGIN, viewportLeft + viewportWidth - width - VIEWPORT_MARGIN)
  );
  const spaceBelow = viewportTop + viewportHeight - rect.bottom - VIEWPORT_MARGIN - DROPDOWN_OFFSET;
  const spaceAbove = rect.top - viewportTop - VIEWPORT_MARGIN - DROPDOWN_OFFSET;
  // 触屏优先在控件下方保留可滚动的选项区，空间不足时才向上展开。
  const direction =
    spaceBelow >= (touch ? 160 : DROPDOWN_MAX_HEIGHT) || spaceBelow >= spaceAbove ? 'down' : 'up';
  const maxHeight = Math.max(
    0,
    Math.min(DROPDOWN_MAX_HEIGHT, direction === 'down' ? spaceBelow : spaceAbove)
  );

  return direction === 'down'
    ? {
        // 使用文档坐标，避开 Safari 可视视口平移时 fixed 元素额外偏移。
        position: 'absolute',
        top: window.scrollY + rect.bottom + DROPDOWN_OFFSET,
        left: window.scrollX + left,
        width,
        maxHeight,
        zIndex: DROPDOWN_Z_INDEX
      }
    : {
        position: 'absolute',
        top: window.scrollY + rect.top - DROPDOWN_OFFSET,
        transform: 'translateY(-100%)',
        left: window.scrollX + left,
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
  renderValue,
  showChevron = true,
  id,
  search,
}: SelectProps) {
  const generatedId = useId();
  const selectId = id ?? generatedId;
  const listboxId = `${selectId}-listbox`;
  const [open, setOpen] = useState(false);
  const [highlightedIndex, setHighlightedIndex] = useState(-1);
  const shouldScrollHighlightRef = useRef(true);
  const [searchQuery, setSearchQuery] = useState('');
  const searchInputRef = useRef<HTMLInputElement | null>(null);
  const triggerRef = useRef<HTMLButtonElement | null>(null);
  const wrapRef = useRef<HTMLDivElement | null>(null);
  const dropdownRef = useRef<HTMLDivElement | null>(null);
  const [dropdownStyle, setDropdownStyle] = useState<CSSProperties | null>(null);
  const isOpen = open && !disabled;
  const searchable = Boolean(search);
  const touch = useMediaQuery('(pointer: coarse)');
  const touchSearch = searchable && touch;
  // 搜索只缩小候选项，提交选择后才更新调用方的筛选值。
  const visibleOptions = useMemo(() => {
    const query = searchable ? searchQuery.trim().toLowerCase() : '';
    return query ? options.filter((option) => option.label.toLowerCase().includes(query)) : options;
  }, [options, searchable, searchQuery]);
  const openDropdown = useCallback(() => {
    shouldScrollHighlightRef.current = true;
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
    document.addEventListener('pointerdown', handleClickOutside);
    return () => document.removeEventListener('pointerdown', handleClickOutside);
  }, [disabled, open]);

  const updateDropdownStyle = useCallback((rect: DOMRect) => {
    setDropdownStyle(resolveDropdownStyle(rect, dropdownMinWidth, touch));
  }, [dropdownMinWidth, touch]);
  useAnchorPosition(isOpen, wrapRef, updateDropdownStyle);

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
      // 触屏返回按钮以收起键盘；桌面保留输入焦点，且不滚动页面。
      if (searchable && !touchSearch) searchInputRef.current?.focus({ preventScroll: true });
      else triggerRef.current?.focus({ preventScroll: true });
      onChange(nextOption.value);
      setOpen(false);
      setHighlightedIndex(nextIndex);
    },
    [onChange, searchable, touchSearch, visibleOptions]
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
      if (['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) shouldScrollHighlightRef.current = true;

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
          // 嵌套在设置弹窗时，Esc 先关闭列表，下一次才交给父弹窗。
          event.stopPropagation();
          if (searchable && !touchSearch) searchInputRef.current?.focus({ preventScroll: true });
          else triggerRef.current?.focus({ preventScroll: true });
          setOpen(false);
          return;
        case 'Tab':
          if (touchSearch && isOpen) {
            if (event.currentTarget === triggerRef.current && !event.shiftKey) {
              event.preventDefault();
              searchInputRef.current?.focus({ preventScroll: true });
              return;
            }
            if (editingSearch) {
              triggerRef.current?.focus({ preventScroll: true });
              if (event.shiftKey) event.preventDefault();
            }
          }
          if (isOpen) setOpen(false);
          return;
        default:
          return;
      }
    },
    [commitSelection, disabled, isOpen, moveHighlight, openDropdown, searchable, touchSearch, visibleOptions.length, resolvedHighlightedIndex]
  );

  useEffect(() => {
    if (!isOpen || resolvedHighlightedIndex < 0 || !shouldScrollHighlightRef.current) return;
    const highlightedOption = document.getElementById(`${selectId}-option-${resolvedHighlightedIndex}`);
    const viewport = dropdownRef.current?.querySelector<HTMLElement>('[data-menu-scroll-viewport]');
    if (!highlightedOption || !viewport) return;
    // 只滚动菜单内部；scrollIntoView 会连带滚动页面，使 iOS 上的锚点跳动。
    const optionRect = highlightedOption.getBoundingClientRect();
    const viewportRect = viewport.getBoundingClientRect();
    if (optionRect.top < viewportRect.top) viewport.scrollTop += optionRect.top - viewportRect.top;
    else if (optionRect.bottom > viewportRect.bottom) viewport.scrollTop += optionRect.bottom - viewportRect.bottom;
  }, [dropdownStyle, isOpen, resolvedHighlightedIndex, selectId, visibleOptions]);

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
        onMouseEnter={opt.disabled ? undefined : () => {
          // 滚动时经过鼠标的选项只高亮，不反向触发自动滚动。
          shouldScrollHighlightRef.current = false;
          setHighlightedIndex(index);
        }}
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

  const searchField = search ? (
    <input
      ref={searchInputRef}
      id={touchSearch ? `${selectId}-search` : selectId}
      className={`${styles.trigger} ${styles.searchInput} ${touchSearch ? styles.touchSearchInput : ''}`}
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
      value={isOpen || touchSearch ? searchQuery : selected?.triggerLabel ?? selected?.label ?? ''}
      autoComplete="off"
      autoCapitalize="none"
      spellCheck={false}
      disabled={disabled}
      onFocus={touchSearch ? undefined : openDropdown}
      onClick={() => {
        if (!isOpen) openDropdown();
      }}
      onChange={(event) => {
        shouldScrollHighlightRef.current = true;
        setSearchQuery(event.target.value);
        setHighlightedIndex(-1);
        setOpen(true);
      }}
      onBlur={(event) => {
        if (touchSearch && !event.relatedTarget) return;
        if (!dropdownRef.current?.contains(event.relatedTarget) && !wrapRef.current?.contains(event.relatedTarget)) setOpen(false);
      }}
      onKeyDown={handleKeyDown}
    />
  ) : null;

  const dropdown =
    isOpen && dropdownStyle
      ? (
          <div
            ref={dropdownRef}
            className={`${styles.dropdown} ${dropdownClassName ?? ''}`.trim()}
            id={searchable ? undefined : listboxId}
            role={searchable ? undefined : 'listbox'}
            aria-label={searchable ? undefined : ariaLabel}
            style={dropdownStyle}
          >
            {touchSearch && <div className={styles.touchSearchField}>{searchField}</div>}
            <MenuScrollArea id={searchable ? listboxId : undefined} role={searchable ? 'listbox' : undefined} ariaLabel={searchable ? ariaLabel : undefined}>
              {optionButtons}
            </MenuScrollArea>
            {search && visibleOptions.length === 0 ? <div role="status" className={styles.noResults}>{search.noResultsText}</div> : null}
          </div>
        )
      : null;

  return (
    <>
      <div
        className={`${styles.wrap} ${fullWidth ? styles.wrapFullWidth : ''} ${className ?? ''}`}
        ref={wrapRef}
      >
        {search && !touchSearch ? (
          <>
            {searchField}
            <span className={`${styles.triggerIcon} ${styles.searchIcon}`} aria-hidden="true">
              <IconChevronDown size={14} />
            </span>
          </>
        ) : <button
          ref={triggerRef}
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
          aria-label={ariaLabel ?? search?.placeholder}
          aria-labelledby={ariaLabelledBy}
          aria-describedby={ariaDescribedBy}
          disabled={disabled}
        >
          <span className={`${styles.triggerText} ${isPlaceholder ? styles.placeholder : ''}`}>
            {renderValue ? renderValue(selected) : displayText}
          </span>
          {showChevron && <span className={styles.triggerIcon} aria-hidden="true">
            <IconChevronDown size={14} />
          </span>}
        </button>}
      </div>
      {dropdown && (typeof document === 'undefined' ? dropdown : createPortal(dropdown, document.body))}
    </>
  );
}
