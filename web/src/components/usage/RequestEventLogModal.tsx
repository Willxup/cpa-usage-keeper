import { useCallback, useEffect, useId, useMemo, useRef, useState } from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/Button'
import { Modal } from '@/components/ui/Modal'
import { IconCheck, IconChevronDown, IconCopy } from '@/components/ui/icons'
import { useScrollBoundaryContainment } from '@/hooks/useScrollBoundaryContainment'
import type { UsageEventRequestLogResponse } from '@/lib/types'
import styles from '@/pages/UsagePage.module.scss'

const REQUEST_LOG_VIRTUAL_LINE_HEIGHT = 18
const REQUEST_LOG_VIRTUAL_OVERSCAN = 8
const REQUEST_LOG_VIRTUAL_PADDING_Y = 12
const REQUEST_LOG_VIRTUAL_CHUNK_CHARS = 2048
const REQUEST_LOG_VIRTUAL_BREAK_LOOKBACK = 256
const REQUEST_LOG_GRAPHEME_CONTEXT_CHARS = 64
const REQUEST_LOG_GRAPHEME_SEGMENTER = typeof Intl !== 'undefined' && typeof Intl.Segmenter === 'function'
  ? new Intl.Segmenter(undefined, { granularity: 'grapheme' })
  : null

const REQUEST_LOG_SECTION_TITLE_KEYS: Record<string, string> = {
  'REQUEST INFO': 'usage_stats.request_events_log_section_request_info',
  HEADERS: 'usage_stats.request_events_log_section_headers',
  'API REQUEST': 'usage_stats.request_events_log_section_api_request',
  'API RESPONSE': 'usage_stats.request_events_log_section_api_response',
  'API RESPONSE ERROR': 'usage_stats.request_events_log_section_api_response_error',
  RESPONSE: 'usage_stats.request_events_log_section_response',
  'WEBSOCKET TIMELINE': 'usage_stats.request_events_log_section_websocket_timeline',
  'API WEBSOCKET TIMELINE': 'usage_stats.request_events_log_section_api_websocket_timeline',
  'RAW LOG': 'usage_stats.request_events_log_section_raw_log',
}

const formatRequestLogSectionTitle = (title: string, translate: (key: string) => string) => {
  const normalizedTitle = title.trim().toUpperCase()
  const translationKey = REQUEST_LOG_SECTION_TITLE_KEYS[normalizedTitle]
  if (translationKey) return translate(translationKey)
  return title.trim() || translate('usage_stats.request_events_log_section')
}

const isPreferredRequestLogChunkBreak = (character: string) => (
  character === ',' || character === '}' || character === ']' || /\s/u.test(character)
)

const findPreferredRequestLogChunkEnd = (content: string, start: number, idealEnd: number) => {
  const minimumEnd = Math.max(
    start + Math.floor((idealEnd - start) * 0.75),
    idealEnd - REQUEST_LOG_VIRTUAL_BREAK_LOOKBACK,
  )
  for (let end = idealEnd; end > minimumEnd; end -= 1) {
    if (isPreferredRequestLogChunkBreak(content[end - 1] ?? '')) return end
  }
  return idealEnd
}

const fallbackRequestLogCodePointBoundary = (content: string, start: number, end: number) => {
  if (end <= start) return start
  const previousCodeUnit = content.charCodeAt(end - 1)
  const nextCodeUnit = content.charCodeAt(end)
  const splitsSurrogatePair = previousCodeUnit >= 0xD800 && previousCodeUnit <= 0xDBFF
    && nextCodeUnit >= 0xDC00 && nextCodeUnit <= 0xDFFF
  return splitsSurrogatePair ? end - 1 : end
}

const findRequestLogGraphemeBoundary = (
  content: string,
  start: number,
  candidateEnd: number,
  lineEnd: number,
) => {
  if (candidateEnd >= lineEnd) return lineEnd
  if (!REQUEST_LOG_GRAPHEME_SEGMENTER) {
    return fallbackRequestLogCodePointBoundary(content, start, candidateEnd)
  }

  // 只分割候选点附近的小窗口，避免对多 MiB 日志逐字执行字素分析。
  const contextStart = Math.max(start, candidateEnd - REQUEST_LOG_GRAPHEME_CONTEXT_CHARS)
  const contextEnd = Math.min(lineEnd, candidateEnd + REQUEST_LOG_GRAPHEME_CONTEXT_CHARS)
  let safeEnd = contextStart
  for (const segment of REQUEST_LOG_GRAPHEME_SEGMENTER.segment(content.slice(contextStart, contextEnd))) {
    const boundary = contextStart + segment.index
    if (boundary > candidateEnd) break
    if (boundary > start) safeEnd = boundary
  }
  if (safeEnd > start) return safeEnd
  return fallbackRequestLogCodePointBoundary(content, start, candidateEnd)
}

export const splitRequestLogVirtualChunks = (
  content: string,
  maxChunkChars = REQUEST_LOG_VIRTUAL_CHUNK_CHARS,
): string[] => {
  if (content === '') return ['']
  const chunkSize = Math.max(2, Math.floor(maxChunkChars))
  const chunks: string[] = []
  let lineStart = 0

  while (lineStart <= content.length) {
    const newlineIndex = content.indexOf('\n', lineStart)
    const lineEnd = newlineIndex === -1 ? content.length : newlineIndex
    if (lineStart === lineEnd) {
      chunks.push('')
    } else {
      let offset = lineStart
      while (offset < lineEnd) {
        const idealEnd = Math.min(offset + chunkSize, lineEnd)
        const preferredEnd = idealEnd < lineEnd
          ? findPreferredRequestLogChunkEnd(content, offset, idealEnd)
          : lineEnd
        const end = findRequestLogGraphemeBoundary(content, offset, preferredEnd, lineEnd)
        chunks.push(content.slice(offset, end))
        offset = end
      }
    }
    if (newlineIndex === -1) break
    lineStart = newlineIndex + 1
  }

  return chunks
}

const copyRequestLogSectionContent = async (content: string) => {
  const clipboard = globalThis.navigator?.clipboard
  if (clipboard) {
    try {
      await clipboard.writeText(content)
      return
    } catch {
      // HTTP 局域网页面可能禁止 Clipboard API，继续使用 textarea 兜底。
    }
  }

  if (typeof document === 'undefined' || typeof document.execCommand !== 'function') {
    throw new Error('clipboard is not available')
  }
  const previouslyFocused = document.activeElement instanceof HTMLElement ? document.activeElement : null
  const textarea = document.createElement('textarea')
  textarea.value = content
  textarea.readOnly = true
  textarea.tabIndex = -1
  textarea.style.position = 'fixed'
  textarea.style.opacity = '0'
  textarea.style.pointerEvents = 'none'
  textarea.style.top = '0'
  textarea.style.left = '0'
  document.body.appendChild(textarea)
  textarea.focus()
  textarea.select()
  try {
    if (!document.execCommand('copy')) throw new Error('copy command failed')
  } finally {
    textarea.remove()
    if (previouslyFocused?.isConnected) previouslyFocused.focus()
  }
}

function RequestLogSectionDisclosure({
  title,
  content,
  defaultOpen,
}: {
  title: string
  content: string
  defaultOpen: boolean
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(defaultOpen)
  const [hasOpened, setHasOpened] = useState(defaultOpen)
  const [copyState, setCopyState] = useState<'idle' | 'copied' | 'failed'>('idle')
  const panelId = useId()
  const scrollerRef = useRef<HTMLDivElement | null>(null)
  const copyResetTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  useScrollBoundaryContainment(scrollerRef)
  const chunks = useMemo(() => hasOpened ? splitRequestLogVirtualChunks(content) : [], [content, hasOpened])
  // TanStack Virtual 依赖内部可变测量状态，不参与 React Compiler 自动记忆化。
  // eslint-disable-next-line react-hooks/incompatible-library
  const rowVirtualizer = useVirtualizer({
    count: hasOpened ? chunks.length : 0,
    getScrollElement: () => scrollerRef.current,
    estimateSize: () => REQUEST_LOG_VIRTUAL_LINE_HEIGHT,
    overscan: REQUEST_LOG_VIRTUAL_OVERSCAN,
    paddingStart: REQUEST_LOG_VIRTUAL_PADDING_Y,
    paddingEnd: REQUEST_LOG_VIRTUAL_PADDING_Y,
    initialRect: { width: 0, height: 360 },
  })
  const virtualItems = rowVirtualizer.getVirtualItems()
  const handleToggle = useCallback(() => {
    const nextOpen = !open
    if (nextOpen) setHasOpened(true)
    setOpen(nextOpen)
  }, [open])
  const handleCopy = useCallback(async () => {
    try {
      await copyRequestLogSectionContent(content)
      setCopyState('copied')
    } catch {
      setCopyState('failed')
    }
    if (copyResetTimerRef.current) clearTimeout(copyResetTimerRef.current)
    copyResetTimerRef.current = setTimeout(() => setCopyState('idle'), 1600)
  }, [content])

  useEffect(() => () => {
    if (copyResetTimerRef.current) clearTimeout(copyResetTimerRef.current)
  }, [])

  const copyLabel = copyState === 'copied'
    ? t('usage_stats.request_events_log_copied_section', { section: title })
    : copyState === 'failed'
      ? t('usage_stats.request_events_log_copy_failed_section', { section: title })
      : t('usage_stats.request_events_log_copy_section', { section: title })

  return (
    <section className={`${styles.requestEventsLogSection} ${open ? styles.requestEventsLogSectionOpen : ''}`.trim()}>
      <div className={styles.requestEventsLogSectionHeader}>
        <button
          type="button"
          className={styles.requestEventsLogSectionTrigger}
          aria-expanded={open}
          aria-controls={panelId}
          onClick={handleToggle}
        >
          <span className={styles.requestEventsLogSectionTitle}>{title}</span>
          <span className={styles.requestEventsLogSectionChevron} aria-hidden="true">
            <IconChevronDown size={14} />
          </span>
        </button>
        <button
          type="button"
          className={`${styles.requestEventsLogSectionCopyButton} ${copyState === 'copied' ? styles.requestEventsLogSectionCopyButtonCopied : ''} ${copyState === 'failed' ? styles.requestEventsLogSectionCopyButtonFailed : ''}`.trim()}
          onClick={() => void handleCopy()}
          aria-label={copyLabel}
          title={copyLabel}
        >
          {copyState === 'copied' ? <IconCheck size={14} /> : <IconCopy size={14} />}
        </button>
      </div>
      <div id={panelId} className={styles.requestEventsLogSectionPanel} aria-hidden={!open}>
        <div className={styles.requestEventsLogSectionPanelInner} ref={scrollerRef}>
          {hasOpened ? (
            <div className={styles.requestEventsLogVirtualSpacer} style={{ height: `${rowVirtualizer.getTotalSize()}px` }}>
              {virtualItems.map((virtualItem) => (
                <pre
                  key={virtualItem.key}
                  ref={rowVirtualizer.measureElement}
                  data-index={virtualItem.index}
                  className={styles.requestEventsLogVirtualLine}
                  style={{ transform: `translateY(${virtualItem.start}px)` }}
                >
                  {chunks[virtualItem.index] || ' '}
                </pre>
              ))}
            </div>
          ) : null}
        </div>
      </div>
    </section>
  )
}

interface RequestEventLogModalProps {
  loadingEventId?: string | null
  response?: UsageEventRequestLogResponse | null
  error?: string
  onClose?: () => void
  onDownload?: (eventId: string) => void
  downloading?: boolean
}

export function RequestEventLogModal({
  loadingEventId = null,
  response = null,
  error = '',
  onClose,
  onDownload,
  downloading = false,
}: RequestEventLogModalProps) {
  const { t } = useTranslation()
  const open = Boolean(response || error || loadingEventId)
  const tooLarge = response?.too_large === true || (response?.previewable === false && response?.downloadable === true)
  const title = tooLarge ? t('usage_stats.request_events_log_too_large_title') : t('usage_stats.request_events_log_title')
  const sections = response?.sections ?? []
  const downloadable = Boolean(response?.downloadable && String(response?.event_id ?? '').trim() && onDownload)
  const handleDownload = useCallback(() => {
    const eventId = String(response?.event_id ?? '').trim()
    if (eventId && onDownload) onDownload(eventId)
  }, [onDownload, response?.event_id])
  const handleClose = onClose ?? (() => undefined)

  return (
    <Modal
      open={open}
      title={title}
      onClose={handleClose}
      width={tooLarge ? 360 : 920}
      className={tooLarge ? styles.requestEventsLargeLogModal : undefined}
      footer={
        tooLarge ? (
          <>
            <Button variant="secondary" size="sm" appearance="action" onClick={handleClose}>
              {t('common.cancel')}
            </Button>
            <Button variant="primary" size="sm" appearance="action" onClick={handleDownload} loading={downloading} disabled={!downloadable}>
              {downloading ? t('common.loading') : t('usage_stats.request_events_log_download')}
            </Button>
          </>
        ) : downloadable ? (
          <Button variant="secondary" size="sm" appearance="action" onClick={handleDownload} loading={downloading}>
            {downloading ? t('common.loading') : t('usage_stats.request_events_log_download')}
          </Button>
        ) : undefined
      }
    >
      <div className={styles.requestEventsLogViewer}>
        {loadingEventId && !response && !error ? (
          <div className={styles.hint} role="status" aria-live="polite">{t('common.loading')}</div>
        ) : error ? (
          <div className={styles.errorBox} role="status" aria-live="polite">{error}</div>
        ) : tooLarge ? (
          <div className={styles.requestEventsLargeLogPrompt} role="status" aria-live="polite">{t('usage_stats.request_events_log_too_large')}</div>
        ) : response ? (
          sections.length > 0 ? (
            <div className={styles.requestEventsLogSections}>
              {sections.map((section, index) => (
                <RequestLogSectionDisclosure
                  key={`${response.event_id}-${section.title}-${index}`}
                  title={formatRequestLogSectionTitle(section.title, t)}
                  content={section.content}
                  defaultOpen={index === 0}
                />
              ))}
            </div>
          ) : (
            <div className={styles.hint}>{t('usage_stats.request_events_log_empty')}</div>
          )
        ) : null}
      </div>
    </Modal>
  )
}
