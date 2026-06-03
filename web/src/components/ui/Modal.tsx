import {
  useCallback,
  useEffect,
  useId,
  useRef,
  useState,
  type MouseEvent,
  type PropsWithChildren,
  type ReactNode,
} from 'react';
import { createPortal } from 'react-dom';
import { useTranslation } from 'react-i18next';
import { IconX } from './icons';

interface ModalProps {
  open: boolean;
  title?: ReactNode;
  onClose: () => void;
  footer?: ReactNode;
  width?: number | string;
  className?: string;
  closeDisabled?: boolean;
}

const CLOSE_ANIMATION_DURATION = 350;
const MODAL_LOCK_CLASS = 'modal-open';
const FOCUSABLE_SELECTOR = [
  'a[href]',
  'button:not([disabled])',
  'input:not([disabled])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(',');
let activeModalCount = 0;

const scrollLockSnapshot = {
  scrollY: 0,
  contentScrollTop: 0,
  contentEl: null as HTMLElement | null,
};

const resolveContentScrollContainer = () => {
  if (typeof document === 'undefined') return null;
  const contentEl = document.querySelector('.content');
  return contentEl instanceof HTMLElement ? contentEl : null;
};

const isModalBodyScrollTarget = (target: EventTarget | null) =>
  target instanceof Element && target.closest('.modal-body') !== null;

const lockScroll = () => {
  if (typeof document === 'undefined') return;
  if (activeModalCount === 0) {
    const html = document.documentElement;
    const contentEl = resolveContentScrollContainer();

    scrollLockSnapshot.scrollY = window.scrollY || window.pageYOffset || html.scrollTop || 0;
    scrollLockSnapshot.contentEl = contentEl;
    scrollLockSnapshot.contentScrollTop = contentEl?.scrollTop ?? 0;

    document.body.classList.add(MODAL_LOCK_CLASS);
    html.classList.add(MODAL_LOCK_CLASS);
  }
  activeModalCount += 1;
};

const unlockScroll = () => {
  if (typeof document === 'undefined') return;
  activeModalCount = Math.max(0, activeModalCount - 1);
  if (activeModalCount === 0) {
    const html = document.documentElement;
    const scrollY = scrollLockSnapshot.scrollY;
    const contentScrollTop = scrollLockSnapshot.contentScrollTop;
    const contentEl = scrollLockSnapshot.contentEl;

    document.body.classList.remove(MODAL_LOCK_CLASS);
    html.classList.remove(MODAL_LOCK_CLASS);

    if (contentEl) {
      contentEl.scrollTo({ top: contentScrollTop, left: 0, behavior: 'auto' });
    }
    window.scrollTo({ top: scrollY, left: 0, behavior: 'auto' });

    scrollLockSnapshot.scrollY = 0;
    scrollLockSnapshot.contentScrollTop = 0;
    scrollLockSnapshot.contentEl = null;
  }
};

export function Modal({
  open,
  title,
  onClose,
  footer,
  width = 520,
  className,
  closeDisabled = false,
  children,
}: PropsWithChildren<ModalProps>) {
  const { t } = useTranslation();
  const titleId = useId();
  const [isVisible, setIsVisible] = useState(false);
  const [isClosing, setIsClosing] = useState(false);
  const closeTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const modalRef = useRef<HTMLDivElement | null>(null);
  const overlayRef = useRef<HTMLDivElement | null>(null);
  const closeButtonRef = useRef<HTMLButtonElement | null>(null);
  const previouslyFocusedRef = useRef<HTMLElement | null>(null);

  const getFocusableElements = useCallback(() => {
    if (!modalRef.current) return [] as HTMLElement[];
    return Array.from(modalRef.current.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR)).filter(
      (element) => !element.hasAttribute('disabled') && element.tabIndex !== -1
    );
  }, []);

  const startClose = useCallback(
    (notifyParent: boolean) => {
      if (closeTimerRef.current !== null) return;
      setIsClosing(true);
      if (notifyParent) {
        onClose();
      }
      closeTimerRef.current = window.setTimeout(() => {
        setIsVisible(false);
        setIsClosing(false);
        closeTimerRef.current = null;
      }, CLOSE_ANIMATION_DURATION);
    },
    [onClose]
  );

  useEffect(() => {
    let cancelled = false;

    if (open) {
      if (closeTimerRef.current !== null) {
        window.clearTimeout(closeTimerRef.current);
        closeTimerRef.current = null;
      }
      queueMicrotask(() => {
        if (cancelled) return;
        setIsVisible(true);
        setIsClosing(false);
      });
    } else if (isVisible) {
      queueMicrotask(() => {
        if (cancelled) return;
        startClose(false);
      });
    }

    return () => {
      cancelled = true;
    };
  }, [open, isVisible, startClose]);

  const handleClose = useCallback(() => {
    startClose(true);
  }, [startClose]);

  const handleOverlayMouseDown = useCallback((event: MouseEvent<HTMLDivElement>) => {
    if (closeDisabled || event.target !== event.currentTarget) return;
    handleClose();
  }, [closeDisabled, handleClose]);

  useEffect(() => {
    return () => {
      if (closeTimerRef.current !== null) {
        window.clearTimeout(closeTimerRef.current);
      }
    };
  }, []);

  const shouldLockScroll = open || isVisible;

  useEffect(() => {
    if (!shouldLockScroll) return;
    lockScroll();
    return () => unlockScroll();
  }, [shouldLockScroll]);

  useEffect(() => {
    if (!open) return;

    previouslyFocusedRef.current =
      document.activeElement instanceof HTMLElement ? document.activeElement : null;

    const focusTimer = window.setTimeout(() => {
      const firstFocusable = getFocusableElements()[0];
      (firstFocusable ?? closeButtonRef.current ?? modalRef.current)?.focus();
    }, 0);

    return () => {
      window.clearTimeout(focusTimer);
    };
  }, [getFocusableElements, open]);

  useEffect(() => {
    if (open || isVisible) return;
    previouslyFocusedRef.current?.focus();
    previouslyFocusedRef.current = null;
  }, [isVisible, open]);

  useEffect(() => {
    if (!open) return;

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        if (closeDisabled) return;
        event.preventDefault();
        handleClose();
        return;
      }

      if (event.key !== 'Tab') return;

      const focusableElements = getFocusableElements();
      if (focusableElements.length === 0) {
        event.preventDefault();
        modalRef.current?.focus();
        return;
      }

      const firstElement = focusableElements[0];
      const lastElement = focusableElements[focusableElements.length - 1];
      const activeElement = document.activeElement as HTMLElement | null;

      if (event.shiftKey) {
        if (activeElement === firstElement || activeElement === modalRef.current) {
          event.preventDefault();
          lastElement.focus();
        }
        return;
      }

      if (activeElement === lastElement) {
        event.preventDefault();
        firstElement.focus();
      }
    };

    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [closeDisabled, getFocusableElements, handleClose, open]);

  useEffect(() => {
    if (!open && !isVisible) return;

    const overlay = overlayRef.current;
    if (!overlay) return;

    const blockOverlayWheel = (event: WheelEvent) => {
      if (isModalBodyScrollTarget(event.target)) return;
      event.preventDefault();
    };
    const blockOverlayTouchMove = (event: TouchEvent) => {
      if (isModalBodyScrollTarget(event.target)) return;
      event.preventDefault();
    };

    // 不改 body 的 inline 布局属性，避免弹窗打开时触发页面跳顶或后台表格宽度重排；只拦截遮罩层自身滚动。
    overlay.addEventListener('wheel', blockOverlayWheel, { passive: false });
    overlay.addEventListener('touchmove', blockOverlayTouchMove, { passive: false });

    return () => {
      overlay.removeEventListener('wheel', blockOverlayWheel);
      overlay.removeEventListener('touchmove', blockOverlayTouchMove);
    };
  }, [isVisible, open]);

  if (!open && !isVisible) return null;

  const overlayClass = `modal-overlay ${isClosing ? 'modal-overlay-closing' : 'modal-overlay-entering'}`;
  const modalClass = `modal ${isClosing ? 'modal-closing' : 'modal-entering'}${className ? ` ${className}` : ''}`;

  const modalContent = (
    <div ref={overlayRef} className={overlayClass} onMouseDown={handleOverlayMouseDown}>
      <div
        ref={modalRef}
        className={modalClass}
        style={{ width, maxWidth: '100%' }}
        role="dialog"
        aria-modal="true"
        aria-labelledby={title ? titleId : undefined}
        tabIndex={-1}
      >
        <button
          ref={closeButtonRef}
          type="button"
          className="modal-close-floating"
          onClick={closeDisabled ? undefined : handleClose}
          aria-label={t('common.close')}
          disabled={closeDisabled}
        >
          <IconX size={20} />
        </button>
        <div className="modal-header">
          <div className="modal-title" id={title ? titleId : undefined}>
            {title}
          </div>
        </div>
        <div className="modal-body">{children}</div>
        {footer && <div className="modal-footer">{footer}</div>}
      </div>
    </div>
  );

  if (typeof document === 'undefined') {
    return modalContent;
  }

  return createPortal(modalContent, document.body);
}
