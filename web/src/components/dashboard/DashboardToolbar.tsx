import { useCallback, useEffect, useId, useLayoutEffect, useRef, useState, type MouseEvent, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { shouldHandleUsageNavigation } from '@/lib/usageNavigation';
import { IconChevronDown, IconChevronsLeft, IconChevronsRight, IconRefreshCw } from '@/components/ui/icons';
import { LoadingSpinner } from '@/components/ui/LoadingSpinner';
import { MenuScrollArea } from '@/components/ui/MenuScrollArea';
import { GlassSurface } from './GlassSurface';
import { BackToTop } from './BackToTop';
import styles from './DashboardToolbar.module.scss';

const COLLAPSED_STORAGE_KEY = 'keeper-dashboard-toolbar-collapsed';
type ToolbarSize = { width: number; height: number };
// Key Viewer 切页会重挂载 Shell，只在同一次提交内交接尺寸与导航焦点。
type ToolbarHandoff = { size: ToolbarSize | null; focusNavigation: boolean };
let pendingLayout: ToolbarHandoff | null = null;

interface DashboardToolbarProps<T extends string> {
  items: ReadonlyArray<{ id: T; label: string; href: string }>;
  activeId: T;
  onNavigate: (id: T) => void;
  filters?: ReactNode[];
  onRefresh: () => void;
  refreshing?: boolean;
  refreshDisabled?: boolean;
}

export function DashboardToolbar<T extends string>({ items, activeId, onNavigate, filters = [], onRefresh, refreshing = false, refreshDisabled = false }: DashboardToolbarProps<T>) {
  const { t, i18n } = useTranslation();
  const [collapsed, setCollapsed] = useState(() => {
    try { return localStorage.getItem(COLLAPSED_STORAGE_KEY) !== 'false'; } catch { return true; }
  });
  const [menuOpen, setMenuOpen] = useState(false);
  const hostRef = useRef<HTMLDivElement>(null);
  const dockRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const selectionRef = useRef<HTMLSpanElement>(null);
  const sizeRef = useRef<ToolbarSize | null>(null);
  const animationRef = useRef<Animation | null>(null);
  const menuId = useId();
  const activeItem = items.find((item) => item.id === activeId) ?? items[0];

  const measure = useCallback(() => {
    const host = hostRef.current;
    const dock = dockRef.current;
    if (!host || !dock || !host.clientWidth) return;
    host.dataset.compact = String(collapsed || host.clientWidth <= 900);
    // 用相同控件的自然宽度决定换行；不压缩查询值，也不重挂载有状态的筛选组件。
    const copy = dock.cloneNode(true) as HTMLDivElement;
    copy.classList.add(styles.measure);
    copy.removeAttribute('data-resizing');
    copy.inert = true;
    copy.setAttribute('aria-hidden', 'true');
    copy.querySelectorAll('[data-glass-surface], [data-dashboard-page-menu], [data-dashboard-selection]').forEach((node) => node.remove());
    copy.querySelectorAll('[id]').forEach((node) => node.removeAttribute('id'));
    host.append(copy);
    const required = copy.getBoundingClientRect().width;
    const fields = copy.querySelectorAll<HTMLElement>('[data-dashboard-filter]');
    const widths = Array.from(fields, (field) => field.getBoundingClientRect().width);
    host.style.setProperty('--toolbar-first-filter', `${widths[0] ?? 0}px`);
    host.style.setProperty('--toolbar-second-filter', `${widths[1] ?? 0}px`);
    host.dataset.flow = required > host.clientWidth + .5 ? 'split' : 'single';
    // 副本测量最终尺寸；动画中的真实尺寸不会反过来触发测量和重新起播。
    copy.classList.remove(styles.measure);
    Object.assign(copy.style, { position: 'absolute', left: '0', top: '0', visibility: 'hidden', pointerEvents: 'none' });
    const target = copy.getBoundingClientRect();
    copy.remove();
    const previous = sizeRef.current;
    if (!previous || Math.abs(previous.width - target.width) > .5 || Math.abs(previous.height - target.height) > .5) {
      const from = animationRef.current ? dock.getBoundingClientRect() : previous;
      animationRef.current?.cancel();
      animationRef.current = null;
      delete dock.dataset.resizing;
      sizeRef.current = { width: target.width, height: target.height };
      if (from && typeof dock.animate === 'function' && !window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
        dock.dataset.resizing = 'true';
        const animation = dock.animate([
          { width: `${from.width}px`, height: `${from.height}px` },
          { width: `${target.width}px`, height: `${target.height}px` },
        ], { duration: 320, easing: 'cubic-bezier(.22, 1, .36, 1)' });
        animationRef.current = animation;
        void animation.finished.then(() => {
          if (animationRef.current === animation) {
            animationRef.current = null;
            delete dock.dataset.resizing;
          }
        }).catch(() => {});
      }
    }
    // Demo 使用一个随活动页移动的白色底片，不再给每个文字节点套胶囊。
    const activeTab = dock.querySelector<HTMLElement>('[data-dashboard-tabs] [aria-selected="true"]');
    if (activeTab?.offsetWidth && selectionRef.current) {
      selectionRef.current.style.width = `${activeTab.offsetWidth}px`;
      selectionRef.current.style.transform = `translateX(${activeTab.offsetLeft}px)`;
    }
  }, [collapsed]);

  useLayoutEffect(() => {
    const handoff = pendingLayout;
    pendingLayout = null;
    if (!sizeRef.current) sizeRef.current = handoff?.size ?? null;
    measure();
    if (handoff?.focusNavigation) {
      const compact = collapsed || (hostRef.current?.clientWidth ?? 0) <= 900;
      const target = compact ? triggerRef.current : dockRef.current?.querySelector<HTMLElement>('[data-dashboard-tabs] [aria-selected="true"]');
      target?.focus({ preventScroll: true });
    }
    const host = hostRef.current;
    const dock = dockRef.current;
    if (!host || !dock) return;
    let availableWidth = host.clientWidth;
    let frame = 0;
    const observer = new ResizeObserver(() => {
      if (host.clientWidth !== availableWidth) {
        availableWidth = host.clientWidth;
        // 单双行切换会改变 host 高度，延后测量避免同一轮尺寸通知反复触发布局。
        cancelAnimationFrame(frame);
        frame = requestAnimationFrame(measure);
      }
    });
    observer.observe(host);
    const mutation = new MutationObserver(measure);
    const controls = dock.querySelector('[data-dashboard-filters]');
    if (controls) mutation.observe(controls, { subtree: true, childList: true, characterData: true });
    void document.fonts?.ready.then(() => { if (host.isConnected) measure(); });
    return () => { observer.disconnect(); mutation.disconnect(); cancelAnimationFrame(frame); };
  }, [measure, collapsed, activeId, i18n.language, filters.length]);

  useLayoutEffect(() => {
    const dock = dockRef.current;
    return () => {
      const size = dock?.getBoundingClientRect();
      const handoff: ToolbarHandoff = {
        size: size?.width ? { width: size.width, height: size.height } : null,
        focusNavigation: !!dock?.querySelector('[data-dashboard-page-trigger]:focus, [data-dashboard-tabs] a:focus'),
      };
      pendingLayout = handoff;
      queueMicrotask(() => { if (pendingLayout === handoff) pendingLayout = null; });
      animationRef.current?.cancel();
    };
  }, []);

  useLayoutEffect(() => {
    const host = hostRef.current;
    const trigger = triggerRef.current;
    const menu = menuRef.current;
    if (!menuOpen || !host || !trigger || !menu) return;
    const positionMenu = () => {
      if (getComputedStyle(trigger).display === 'none') {
        setMenuOpen(false);
        return;
      }
      // 菜单留在动画裁切层外，左沿跟随 Tab 按钮；空间不足时收回页面边界内。
      const offset = trigger.getBoundingClientRect().left - host.getBoundingClientRect().left;
      const available = Math.max(0, host.clientWidth - menu.offsetWidth);
      menu.style.left = `${Math.max(0, Math.min(offset, available))}px`;
    };
    positionMenu();
    let frame = 0;
    // 尺寸变化后在下一帧更新位置，避免在观察回调内再次触发布局通知。
    const observer = new ResizeObserver(() => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(positionMenu);
    });
    [host, trigger, menu].forEach((element) => observer.observe(element));
    return () => { observer.disconnect(); cancelAnimationFrame(frame); };
  }, [menuOpen]);

  useEffect(() => {
    if (!menuOpen) return;
    menuRef.current?.querySelector<HTMLElement>('[aria-current="page"]')?.focus();
    const onPointer = (event: PointerEvent) => {
      const target = event.target as Node;
      if (!menuRef.current?.contains(target) && !triggerRef.current?.contains(target)) setMenuOpen(false);
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') { event.preventDefault(); setMenuOpen(false); triggerRef.current?.focus(); }
    };
    document.addEventListener('pointerdown', onPointer);
    document.addEventListener('keydown', onKey);
    return () => { document.removeEventListener('pointerdown', onPointer); document.removeEventListener('keydown', onKey); };
  }, [menuOpen]);

  const activatePage = (id: T) => {
    // 选择项卸载前恢复顺序焦点，避免下一次 Tab 跳过全局筛选并滚到内容区。
    if (menuOpen) triggerRef.current?.focus({ preventScroll: true });
    setMenuOpen(false);
    if (id === activeId) return;
    onNavigate(id);
    // 独立页面从顶部开始；当前页、筛选和刷新不改变阅读位置。
    window.scrollTo({ top: 0, behavior: 'instant' });
  };

  const navigate = (event: MouseEvent<HTMLAnchorElement>, id: T) => {
    if (!shouldHandleUsageNavigation(event.nativeEvent)) return;
    event.preventDefault();
    activatePage(id);
  };

  return (
    <div ref={hostRef} className={styles.host} data-dashboard-toolbar data-collapsed={collapsed} lang={i18n.resolvedLanguage || i18n.language}>
      <div ref={dockRef} className={styles.dock}>
        <GlassSurface />
        <button type="button" data-dashboard-collapse className={styles.collapse} aria-expanded={!collapsed} aria-label={t(collapsed ? 'usage_stats.expand_toolbar' : 'usage_stats.collapse_toolbar')} onClick={() => {
          const next = !collapsed; setCollapsed(next); setMenuOpen(false);
          try { localStorage.setItem(COLLAPSED_STORAGE_KEY, String(next)); } catch { /* 禁用存储时仍允许本次操作。 */ }
        }}>
          {collapsed ? <IconChevronsRight size={20} /> : <IconChevronsLeft size={20} />}
        </button>
        <nav className={styles.tabs} data-dashboard-tabs role="tablist" aria-label={t('usage_stats.tabs_aria_label')}>
          <span ref={selectionRef} className={styles.selection} data-dashboard-selection aria-hidden="true" />
          {items.map((item) => <a key={item.id} href={item.href} role="tab" aria-selected={item.id === activeId} className={styles.tab} onClick={(event) => navigate(event, item.id)} onKeyDown={(event) => {
            if (event.key === ' ') { event.preventDefault(); activatePage(item.id); }
          }}><span>{item.label}</span></a>)}
        </nav>
        <button ref={triggerRef} type="button" className={styles.pageTrigger} data-dashboard-page-trigger aria-label={`${activeItem?.label} · ${t('usage_stats.switch_page')}`} aria-haspopup="menu" aria-expanded={menuOpen} aria-controls={menuId} onClick={() => setMenuOpen((open) => !open)} onKeyDown={(event) => {
          if (event.key === 'ArrowDown' || event.key === 'ArrowUp') { event.preventDefault(); setMenuOpen(true); }
        }}>
          <span className={styles.currentPage} data-dashboard-page-label>{activeItem?.label}</span>
          <IconChevronDown size={14} aria-hidden="true" />
        </button>
        {filters.length > 0 && <div className={styles.filters} data-dashboard-filters data-count={filters.length}>
          {filters.map((filter, index) => <div className={styles.filter} key={index} data-dashboard-filter>
            {filter}
          </div>)}
        </div>}
        <button type="button" className={styles.refresh} data-dashboard-refresh aria-label={t('usage_stats.refresh')} aria-busy={refreshing} title={t('usage_stats.refresh')} disabled={refreshDisabled || refreshing} onClick={onRefresh}>
          {refreshing ? <LoadingSpinner size={16} /> : <IconRefreshCw size={16} aria-hidden="true" />}
        </button>
      </div>
        {menuOpen && <div ref={menuRef} id={menuId} className={styles.pageMenu} data-dashboard-page-menu role="menu" aria-label={t('usage_stats.tabs_aria_label')} onKeyDown={(event) => {
          const choices = Array.from(menuRef.current?.querySelectorAll<HTMLAnchorElement>('a') ?? []);
          const index = choices.indexOf(document.activeElement as HTMLAnchorElement);
          const next = event.key === 'ArrowDown' ? (index + 1) % choices.length : event.key === 'ArrowUp' ? (index + choices.length - 1) % choices.length : event.key === 'Home' ? 0 : event.key === 'End' ? choices.length - 1 : -1;
          if (next >= 0) { event.preventDefault(); choices[next]?.focus(); }
        }} onBlur={(event) => {
          // Safari 触摸可能在 click 前失焦且 relatedTarget 为空；外部点击由 pointerdown 判断。
          if (event.relatedTarget && !event.currentTarget.contains(event.relatedTarget) && !triggerRef.current?.contains(event.relatedTarget)) setMenuOpen(false);
        }}>
          <MenuScrollArea>
          {items.map((item) => <a key={item.id} role="menuitem" href={item.href} aria-current={item.id === activeId ? 'page' : undefined} onClick={(event) => navigate(event, item.id)} onKeyDown={(event) => {
            if (event.key === ' ') { event.preventDefault(); activatePage(item.id); }
          }}>{item.label}</a>)}
          </MenuScrollArea>
        </div>}
      <BackToTop />
    </div>
  );
}
