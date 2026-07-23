import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { flushSync } from 'react-dom';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Modal } from '@/components/ui/Modal';
import { IconGripVertical } from '@/components/ui/icons';
import styles from '@/pages/UsagePage.module.scss';
import {
  moveRequestEventColumnId,
  normalizeRequestEventColumnOrder,
  normalizeRequestEventVisibleColumnIds,
  toggleRequestEventColumnId,
  type RequestEventColumnId,
} from './requestEventColumns';

const COLUMN_DRAG_AUTO_SCROLL_EDGE_PX = 48;
const COLUMN_DRAG_AUTO_SCROLL_MAX_STEP_PX = 14;
const COLUMN_REORDER_ANIMATION_DURATION_MS = 120;
const COLUMN_DRAG_SNAP_DURATION_MS = 100;
const COLUMN_REORDER_ANIMATION_EASING = 'cubic-bezier(0.22, 1, 0.36, 1)';

const getColumnDragAutoScrollDelta = (containerRect: DOMRect, pointerY: number) => {
  if (pointerY < containerRect.top + COLUMN_DRAG_AUTO_SCROLL_EDGE_PX) {
    const intensity = Math.min(1, (containerRect.top + COLUMN_DRAG_AUTO_SCROLL_EDGE_PX - pointerY) / COLUMN_DRAG_AUTO_SCROLL_EDGE_PX);
    return -Math.max(1, Math.ceil(intensity * COLUMN_DRAG_AUTO_SCROLL_MAX_STEP_PX));
  }
  if (pointerY > containerRect.bottom - COLUMN_DRAG_AUTO_SCROLL_EDGE_PX) {
    const intensity = Math.min(1, (pointerY - containerRect.bottom + COLUMN_DRAG_AUTO_SCROLL_EDGE_PX) / COLUMN_DRAG_AUTO_SCROLL_EDGE_PX);
    return Math.max(1, Math.ceil(intensity * COLUMN_DRAG_AUTO_SCROLL_MAX_STEP_PX));
  }
  return 0;
};

export type RequestEventColumnOption = {
  id: RequestEventColumnId;
  label: string;
};

interface RequestEventsColumnSettingsModalProps {
  open: boolean;
  options: readonly RequestEventColumnOption[];
  visibleColumnIds: readonly RequestEventColumnId[];
  columnOrder: readonly RequestEventColumnId[];
  onApply: (visibleColumnIds: RequestEventColumnId[], columnOrder: RequestEventColumnId[]) => void;
  onClose: () => void;
}

export function RequestEventsColumnSettingsModal({
  open,
  options,
  visibleColumnIds,
  columnOrder,
  onApply,
  onClose,
}: RequestEventsColumnSettingsModalProps) {
  const { t } = useTranslation();
  const availableColumnIds = useMemo(() => options.map((option) => option.id), [options]);
  const [draftVisibleColumnIds, setDraftVisibleColumnIds] = useState<RequestEventColumnId[]>(() => (
    normalizeRequestEventVisibleColumnIds(visibleColumnIds, availableColumnIds)
  ));
  const [draftColumnOrder, setDraftColumnOrder] = useState<RequestEventColumnId[]>(() => (
    normalizeRequestEventColumnOrder(columnOrder, availableColumnIds)
  ));
  const draftColumnOrderRef = useRef(draftColumnOrder);
  const [moveAnnouncement, setMoveAnnouncement] = useState('');
  const listRef = useRef<HTMLDivElement | null>(null);
  const rowElementsRef = useRef(new Map<RequestEventColumnId, HTMLDivElement>());
  const rowPositionsRef = useRef(new Map<RequestEventColumnId, number>());
  const rowAnimationsRef = useRef(new Map<RequestEventColumnId, Animation>());
  const draggingColumnIdRef = useRef<RequestEventColumnId | null>(null);
  const dragPointerIdRef = useRef<number | null>(null);
  const draggedRowElementRef = useRef<HTMLDivElement | null>(null);
  const dragPointerPositionRef = useRef<{ x: number; y: number } | null>(null);
  const dragPointerGrabOffsetYRef = useRef(0);
  const dragTranslateYRef = useRef(0);
  const dragAnimationFrameRef = useRef<number | null>(null);
  const dragSnapAnimationRef = useRef<Animation | null>(null);
  const dragScrollContainerRef = useRef<HTMLElement | null>(null);
  const dragScrollListenerRef = useRef<(() => void) | null>(null);
  const dragWheelListenerRef = useRef<((event: WheelEvent) => void) | null>(null);
  const dragWheelDirectionRef = useRef<-1 | 0 | 1>(0);
  const keyboardMovedColumnIdRef = useRef<RequestEventColumnId | null>(null);
  const [draggingColumnId, setDraggingColumnId] = useState<RequestEventColumnId | null>(null);
  const optionsById = useMemo(
    () => new Map(options.map((option) => [option.id, option])),
    [options],
  );
  const orderedOptions = draftColumnOrder.flatMap((columnId) => {
    const option = optionsById.get(columnId);
    return option ? [option] : [];
  });
  const visibleColumnIdSet = useMemo(
    () => new Set<RequestEventColumnId>(draftVisibleColumnIds),
    [draftVisibleColumnIds],
  );

  useLayoutEffect(() => {
    draftColumnOrderRef.current = draftColumnOrder;
  }, [draftColumnOrder]);

  const reduceMotion = () => typeof window !== 'undefined'
    && window.matchMedia?.('(prefers-reduced-motion: reduce)').matches === true;

  const syncDraggedRowTransform = () => {
    const rowElement = draggedRowElementRef.current;
    const pointerPosition = dragPointerPositionRef.current;
    if (!rowElement || !pointerPosition || !draggingColumnIdRef.current) return;

    // getBoundingClientRect 包含当前 transform；先还原布局坐标，再只写一次合成层位移。
    const layoutTop = rowElement.getBoundingClientRect().top - dragTranslateYRef.current;
    const nextTranslateY = pointerPosition.y - dragPointerGrabOffsetYRef.current - layoutTop;
    if (Math.abs(nextTranslateY - dragTranslateYRef.current) < 0.1) return;

    dragTranslateYRef.current = nextTranslateY;
    rowElement.style.transform = `translate3d(0, ${nextTranslateY}px, 0)`;
  };

  useLayoutEffect(() => {
    const listTop = listRef.current?.getBoundingClientRect().top ?? 0;
    const shouldReduceMotion = reduceMotion();
    const previousPositions = rowPositionsRef.current;
    const nextPositions = new Map<RequestEventColumnId, number>();
    const activeDraggedColumnId = draggingColumnIdRef.current;

    for (const columnId of draftColumnOrder) {
      const rowElement = rowElementsRef.current.get(columnId);
      if (!rowElement) continue;

      const runningAnimation = rowAnimationsRef.current.get(columnId);
      if (columnId === activeDraggedColumnId) {
        runningAnimation?.cancel();
        rowAnimationsRef.current.delete(columnId);
        nextPositions.set(
          columnId,
          rowElement.getBoundingClientRect().top - dragTranslateYRef.current - listTop,
        );
        continue;
      }

      const visualTop = runningAnimation
        ? rowElement.getBoundingClientRect().top - listTop
        : null;
      runningAnimation?.cancel();
      rowAnimationsRef.current.delete(columnId);

      const nextTop = rowElement.getBoundingClientRect().top - listTop;
      nextPositions.set(columnId, nextTop);
      const previousTop = previousPositions.get(columnId);
      const translateY = (visualTop ?? previousTop ?? nextTop) - nextTop;
      if (shouldReduceMotion || Math.abs(translateY) < 0.5 || typeof rowElement.animate !== 'function') continue;

      const animation = rowElement.animate(
        [
          { transform: `translate3d(0, ${translateY}px, 0)` },
          { transform: 'translate3d(0, 0, 0)' },
        ],
        {
          duration: COLUMN_REORDER_ANIMATION_DURATION_MS,
          easing: COLUMN_REORDER_ANIMATION_EASING,
        },
      );
      rowAnimationsRef.current.set(columnId, animation);
      const clearFinishedAnimation = () => {
        if (rowAnimationsRef.current.get(columnId) === animation) {
          rowAnimationsRef.current.delete(columnId);
        }
      };
      animation.onfinish = clearFinishedAnimation;
      animation.oncancel = clearFinishedAnimation;
    }

    rowPositionsRef.current = nextPositions;
    syncDraggedRowTransform();
    const keyboardMovedColumnId = keyboardMovedColumnIdRef.current;
    if (keyboardMovedColumnId) {
      keyboardMovedColumnIdRef.current = null;
      rowElementsRef.current.get(keyboardMovedColumnId)?.scrollIntoView?.({ block: 'nearest' });
    }
  }, [draftColumnOrder]);

  const moveColumn = (option: RequestEventColumnOption, targetIndex: number) => {
    const nextColumnOrder = moveRequestEventColumnId(draftColumnOrder, option.id, targetIndex);
    const nextIndex = nextColumnOrder.indexOf(option.id);
    if (nextIndex === draftColumnOrder.indexOf(option.id)) return;
    keyboardMovedColumnIdRef.current = option.id;
    draftColumnOrderRef.current = nextColumnOrder;
    setDraftColumnOrder(nextColumnOrder);
    setMoveAnnouncement(t('usage_stats.request_events_column_moved', {
      column: option.label,
      position: nextIndex + 1,
      total: nextColumnOrder.length,
    }));
  };

  const moveDraggedColumnFromPoint = (clientX: number, clientY: number, allowNearestFallback = false) => {
    const columnId = draggingColumnIdRef.current;
    if (!columnId || typeof document.elementFromPoint !== 'function') return;
    const currentColumnOrder = draftColumnOrderRef.current;
    const currentIndex = currentColumnOrder.indexOf(columnId);
    if (currentIndex < 0) return;

    const target = document.elementFromPoint(clientX, clientY)?.closest<HTMLElement>(
      '[data-request-events-column-row]',
    );
    const targetColumnId = target?.dataset.requestEventsColumnRow as RequestEventColumnId | undefined;
    let targetIndex = targetColumnId ? currentColumnOrder.indexOf(targetColumnId) : -1;

    if (target && targetIndex >= 0 && targetIndex !== currentIndex) {
      const targetRect = target.getBoundingClientRect();
      const targetMidpointY = targetRect.top + targetRect.height / 2;
      if (currentIndex < targetIndex && clientY < targetMidpointY) {
        targetIndex -= 1;
      } else if (currentIndex > targetIndex && clientY > targetMidpointY) {
        targetIndex += 1;
      }
    } else {
      if (!allowNearestFallback) return;
      // 指针落在行间或列表边界时，才按可见行中点计算插入位，避免增加正常热路径的测量。
      targetIndex = 0;
      let measuredRowCount = 0;
      for (const currentColumnId of currentColumnOrder) {
        if (currentColumnId === columnId) continue;
        const rowElement = rowElementsRef.current.get(currentColumnId);
        if (!rowElement) continue;
        measuredRowCount += 1;
        const rowRect = rowElement.getBoundingClientRect();
        if (clientY >= rowRect.top + rowRect.height / 2) {
          targetIndex += 1;
        }
      }
      if (measuredRowCount === 0) return;
    }

    if (targetIndex === currentIndex) return;

    const nextColumnOrder = moveRequestEventColumnId(currentColumnOrder, columnId, targetIndex);
    if (nextColumnOrder === currentColumnOrder) return;
    draftColumnOrderRef.current = nextColumnOrder;
    setDraftColumnOrder(nextColumnOrder);
  };

  const getModalBody = () => listRef.current?.closest<HTMLElement>('.modal-body') ?? null;

  const cancelDragFrame = () => {
    if (dragAnimationFrameRef.current !== null) {
      window.cancelAnimationFrame(dragAnimationFrameRef.current);
      dragAnimationFrameRef.current = null;
    }
  };

  const scheduleDragFrame = () => {
    if (dragAnimationFrameRef.current !== null || !draggingColumnIdRef.current) return;

    dragAnimationFrameRef.current = window.requestAnimationFrame(() => {
      dragAnimationFrameRef.current = null;
      const pointerPosition = dragPointerPositionRef.current;
      if (!draggingColumnIdRef.current || !pointerPosition) return;

      let didAutoScroll = false;
      const modalBody = getModalBody();
      if (modalBody) {
        let scrollDelta = getColumnDragAutoScrollDelta(
          modalBody.getBoundingClientRect(),
          pointerPosition.y,
        );
        if (
          scrollDelta !== 0
          && dragWheelDirectionRef.current !== 0
          && Math.sign(scrollDelta) !== dragWheelDirectionRef.current
        ) {
          scrollDelta = 0;
        }
        const maxScrollTop = Math.max(0, modalBody.scrollHeight - modalBody.clientHeight);
        const nextScrollTop = Math.min(maxScrollTop, Math.max(0, modalBody.scrollTop + scrollDelta));
        if (nextScrollTop !== modalBody.scrollTop) {
          modalBody.scrollTop = nextScrollTop;
          didAutoScroll = true;
        }
      }

      syncDraggedRowTransform();
      moveDraggedColumnFromPoint(pointerPosition.x, pointerPosition.y);
      if (didAutoScroll) scheduleDragFrame();
    });
  };

  const detachDragScrollListener = () => {
    const scrollContainer = dragScrollContainerRef.current;
    const scrollListener = dragScrollListenerRef.current;
    const wheelListener = dragWheelListenerRef.current;
    if (scrollContainer && scrollListener) {
      scrollContainer.removeEventListener('scroll', scrollListener);
    }
    if (scrollContainer && wheelListener) {
      scrollContainer.removeEventListener('wheel', wheelListener);
    }
    dragScrollContainerRef.current = null;
    dragScrollListenerRef.current = null;
    dragWheelListenerRef.current = null;
    dragWheelDirectionRef.current = 0;
  };

  const attachDragScrollListener = () => {
    detachDragScrollListener();
    const scrollContainer = getModalBody();
    if (!scrollContainer) return;

    // 滚轮不会触发 Pointer Move，仍复用同一 rAF 队列校正拖动行位置。
    const scrollListener = () => scheduleDragFrame();
    const wheelListener = (event: WheelEvent) => {
      if (event.deltaY === 0) return;
      dragWheelDirectionRef.current = event.deltaY > 0 ? 1 : -1;
      scheduleDragFrame();
    };
    dragScrollContainerRef.current = scrollContainer;
    dragScrollListenerRef.current = scrollListener;
    dragWheelListenerRef.current = wheelListener;
    scrollContainer.addEventListener('scroll', scrollListener, { passive: true });
    scrollContainer.addEventListener('wheel', wheelListener, { passive: true });
  };

  const clearPointerDrag = () => {
    detachDragScrollListener();
    if (!draggingColumnIdRef.current && !draggedRowElementRef.current) return;
    cancelDragFrame();
    const rowElement = draggedRowElementRef.current;
    const currentTransform = rowElement?.style.transform ?? '';
    dragSnapAnimationRef.current?.cancel();
    dragSnapAnimationRef.current = null;

    if (rowElement) {
      if (
        currentTransform
        && !reduceMotion()
        && typeof rowElement.animate === 'function'
      ) {
        const animation = rowElement.animate(
          [
            { transform: currentTransform },
            { transform: 'translate3d(0, 0, 0)' },
          ],
          {
            duration: COLUMN_DRAG_SNAP_DURATION_MS,
            easing: COLUMN_REORDER_ANIMATION_EASING,
          },
        );
        dragSnapAnimationRef.current = animation;
        const clearFinishedAnimation = () => {
          if (dragSnapAnimationRef.current === animation) {
            dragSnapAnimationRef.current = null;
          }
        };
        animation.onfinish = clearFinishedAnimation;
        animation.oncancel = clearFinishedAnimation;
      }
      rowElement.style.transform = '';
    }

    dragPointerPositionRef.current = null;
    draggingColumnIdRef.current = null;
    dragPointerIdRef.current = null;
    draggedRowElementRef.current = null;
    dragPointerGrabOffsetYRef.current = 0;
    dragTranslateYRef.current = 0;
    setDraggingColumnId(null);
  };

  useEffect(() => () => {
    if (dragAnimationFrameRef.current !== null) {
      window.cancelAnimationFrame(dragAnimationFrameRef.current);
    }
    dragSnapAnimationRef.current?.cancel();
    const scrollContainer = dragScrollContainerRef.current;
    const scrollListener = dragScrollListenerRef.current;
    const wheelListener = dragWheelListenerRef.current;
    if (scrollContainer && scrollListener) {
      scrollContainer.removeEventListener('scroll', scrollListener);
    }
    if (scrollContainer && wheelListener) {
      scrollContainer.removeEventListener('wheel', wheelListener);
    }
    if (draggedRowElementRef.current) draggedRowElementRef.current.style.transform = '';
    for (const animation of rowAnimationsRef.current.values()) {
      animation.cancel();
    }
  }, []);

  const finishPointerDrag = (event: React.PointerEvent<HTMLDivElement>) => {
    if (event.pointerId !== dragPointerIdRef.current) return;
    if (draggingColumnIdRef.current && event.type === 'pointerup') {
      dragPointerPositionRef.current = { x: event.clientX, y: event.clientY };
      cancelDragFrame();
      syncDraggedRowTransform();
      // 松手可能早于下一帧；先提交最后一次命中，避免快速拖放漏掉目标位置。
      flushSync(() => moveDraggedColumnFromPoint(event.clientX, event.clientY, true));
    }
    if (event.currentTarget.hasPointerCapture?.(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId);
    }
    clearPointerDrag();
  };

  const handlePointerMove = (event: React.PointerEvent<HTMLDivElement>) => {
    if (!draggingColumnIdRef.current || event.pointerId !== dragPointerIdRef.current) return;
    dragWheelDirectionRef.current = 0;
    dragPointerPositionRef.current = { x: event.clientX, y: event.clientY };
    scheduleDragFrame();
  };

  const handleLostPointerCapture = (event: React.PointerEvent<HTMLDivElement>) => {
    if (event.pointerId !== dragPointerIdRef.current) return;
    clearPointerDrag();
  };

  return (
    <Modal
      open={open}
      title={t('usage_stats.request_events_column_settings_title')}
      onClose={onClose}
      width={560}
      className={styles.requestEventsColumnSettingsModal}
      footer={(
        <>
          <Button
            type="button"
            variant="secondary"
            size="sm"
            className={styles.requestEventsColumnSettingsAction}
            data-request-events-column-settings-cancel
            onClick={onClose}
          >
            {t('common.cancel')}
          </Button>
          <Button
            type="button"
            variant="primary"
            size="sm"
            className={styles.requestEventsColumnSettingsAction}
            data-request-events-column-settings-apply
            onClick={() => {
              onApply(draftVisibleColumnIds, draftColumnOrder);
              onClose();
            }}
          >
            {t('common.apply')}
          </Button>
        </>
      )}
    >
      <div className={styles.requestEventsColumnSettingsPanel}>
        <p className={styles.requestEventsColumnSettingsDescription}>
          {t('usage_stats.request_events_column_settings_description')}
        </p>
        <div
          className={styles.requestEventsColumnSettingsAnnouncement}
          role="status"
          aria-live="polite"
          aria-atomic="true"
          data-request-events-column-move-announcement
        >
          {moveAnnouncement}
        </div>
        <div
          ref={listRef}
          className={styles.requestEventsColumnSettingsList}
          data-request-events-column-settings-list
          onPointerMove={handlePointerMove}
          onPointerUp={finishPointerDrag}
          onPointerCancel={finishPointerDrag}
          onLostPointerCapture={handleLostPointerCapture}
        >
          {orderedOptions.map((option, index) => {
            const selected = visibleColumnIdSet.has(option.id);
            const visibilityDisabled = selected && draftVisibleColumnIds.length === 1;
            return (
              <div
                key={option.id}
                ref={(element) => {
                  if (element) {
                    rowElementsRef.current.set(option.id, element);
                  } else {
                    rowElementsRef.current.delete(option.id);
                  }
                }}
                className={`${styles.requestEventsColumnSettingsRow} ${draggingColumnId === option.id ? styles.requestEventsColumnSettingsRowDragging : ''}`.trim()}
                data-request-events-column-row={option.id}
              >
                <button
                  type="button"
                  className={styles.requestEventsColumnDragHandle}
                  data-request-events-column-drag-handle={option.id}
                  aria-label={t('usage_stats.request_events_column_move', {
                    column: option.label,
                    position: index + 1,
                    total: orderedOptions.length,
                  })}
                  aria-keyshortcuts="ArrowUp ArrowDown"
                  onKeyDown={(event) => {
                    if (event.key !== 'ArrowUp' && event.key !== 'ArrowDown') return;
                    event.preventDefault();
                    moveColumn(option, index + (event.key === 'ArrowUp' ? -1 : 1));
                  }}
                  onPointerDown={(event) => {
                    if (!event.isPrimary || event.button !== 0 || draggingColumnIdRef.current) return;
                    event.preventDefault();
                    const rowElement = rowElementsRef.current.get(option.id);
                    if (!rowElement) return;

                    const visualTop = rowElement.getBoundingClientRect().top;
                    dragSnapAnimationRef.current?.cancel();
                    dragSnapAnimationRef.current = null;
                    rowAnimationsRef.current.get(option.id)?.cancel();
                    rowAnimationsRef.current.delete(option.id);
                    const layoutTop = rowElement.getBoundingClientRect().top;
                    const initialTranslateY = visualTop - layoutTop;
                    rowElement.style.transform = Math.abs(initialTranslateY) < 0.1
                      ? ''
                      : `translate3d(0, ${initialTranslateY}px, 0)`;
                    draggingColumnIdRef.current = option.id;
                    dragPointerIdRef.current = event.pointerId;
                    draggedRowElementRef.current = rowElement;
                    dragPointerPositionRef.current = { x: event.clientX, y: event.clientY };
                    dragPointerGrabOffsetYRef.current = event.clientY - visualTop;
                    dragTranslateYRef.current = initialTranslateY;
                    attachDragScrollListener();
                    setDraggingColumnId(option.id);
                    listRef.current?.setPointerCapture?.(event.pointerId);
                  }}
                >
                  <IconGripVertical size={16} />
                </button>
                <span className={styles.requestEventsColumnSettingsLabel}>{option.label}</span>
                <label className={`${styles.requestEventsColumnVisibilityControl} ${visibilityDisabled ? styles.requestEventsColumnVisibilityControlDisabled : ''}`.trim()}>
                  <input
                    type="checkbox"
                    checked={selected}
                    disabled={visibilityDisabled}
                    data-request-events-column-visibility={option.id}
                    aria-label={t('usage_stats.request_events_column_visibility', { column: option.label })}
                    onChange={() => {
                      setDraftVisibleColumnIds((currentVisibleColumnIds) => (
                        toggleRequestEventColumnId(currentVisibleColumnIds, option.id, draftColumnOrder)
                      ));
                    }}
                  />
                  <span className={styles.requestEventsColumnVisibilityTrack} aria-hidden="true">
                    <span className={styles.requestEventsColumnVisibilityThumb} />
                  </span>
                </label>
              </div>
            );
          })}
        </div>
      </div>
    </Modal>
  );
}
