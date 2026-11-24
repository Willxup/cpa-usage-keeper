import { useCallback, useEffect, useId, useLayoutEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/Button'
import { PortalTooltip, usePortalTooltip } from '@/components/ui/PortalTooltip'
import { useMediaQuery } from '@/hooks/useMediaQuery'
import type { UsageIdentityModelsRange, UsageIdentityModelTotals } from '@/lib/types'
import { calculateCacheReadRate, calculateDisplayInputTokens, calculateDisplayOutputTokens, formatCompactTokenValue, formatUsd } from '@/utils/usage'
import { USAGE_CHART_TOKEN_COLORS } from '@/utils/usage/chartConfig'
import { cacheReadRateTone, credentialToneClassName, formatCredentialNumber, formatCredentialPercent } from './credentialMetrics'
import { useCredentialModelUsage } from './useCredentialModelUsage'
import styles from './CredentialModelUsage.module.scss'

const RANGES: UsageIdentityModelsRange[] = ['today', '7d', '30d']
const TOKEN_PARTS = [
  { key: 'input', label: 'usage_stats.credentials_models_input', tooltipLabel: 'usage_stats.input_tokens', rawField: 'input_tokens', color: USAGE_CHART_TOKEN_COLORS.input.base },
  { key: 'cacheRead', label: 'usage_stats.credentials_models_cache_read', tooltipLabel: 'usage_stats.cache_read_tokens', rawField: 'cache_read_tokens', color: USAGE_CHART_TOKEN_COLORS.cacheRead.base },
  { key: 'cacheCreation', label: 'usage_stats.credentials_models_cache_write', tooltipLabel: 'usage_stats.cache_creation_tokens', rawField: 'cache_creation_tokens', color: USAGE_CHART_TOKEN_COLORS.cacheWrite.base },
  { key: 'output', label: 'usage_stats.credentials_models_output', tooltipLabel: 'usage_stats.output_tokens', rawField: 'output_tokens', color: USAGE_CHART_TOKEN_COLORS.output.base },
  { key: 'reasoning', label: 'usage_stats.credentials_models_reasoning', tooltipLabel: 'usage_stats.reasoning_tokens', rawField: 'reasoning_tokens', color: USAGE_CHART_TOKEN_COLORS.reasoning.base },
] as const

interface CredentialModelUsageProps {
  identityId: string
  onAuthRequired?: () => void
  enabled?: boolean
}

export function CredentialModelUsage(props: CredentialModelUsageProps) {
  // 身份改变时重置范围，同时立即卸载旧列表及其 Portal tooltip。
  return <ModelUsage key={props.identityId} {...props} />
}

function ModelUsage({ identityId, onAuthRequired, enabled = true }: CredentialModelUsageProps) {
  const { t } = useTranslation()
  const isMobile = useMediaQuery('(max-width: 768px)')
  const [selection, setSelection] = useState<{ range: UsageIdentityModelsRange; revision: number }>({ range: '7d', revision: 0 })
  const { range } = selection
  const [minimumHeight, setMinimumHeight] = useState(0)
  const panelRef = useRef<HTMLElement>(null)
  const requiredScrollHeight = useCallback(() => {
    const panel = panelRef.current
    const scroller = panel?.closest<HTMLElement>('.modal-body')
    if (!panel || !scroller) return null
    if (scroller.scrollTop <= 0) return 0
    return Math.max(0, Math.ceil(panel.getBoundingClientRect().height + scroller.scrollTop + scroller.clientHeight - scroller.scrollHeight))
  }, [])
  const preserveScrollPosition = useCallback(() => {
    const required = requiredScrollHeight()
    if (required !== null) setMinimumHeight(required)
  }, [requiredScrollHeight])
  useLayoutEffect(() => {
    if (!enabled || minimumHeight <= 0) return
    const scroller = panelRef.current?.closest<HTMLElement>('.modal-body')
    if (!scroller) return
    // 用户向上滚动时逐步释放维持当前位置所需的空白，不固定完整模型表高度。
    const release = () => {
      const required = requiredScrollHeight()
      if (required !== null) setMinimumHeight(current => Math.min(current, required))
    }
    scroller.addEventListener('scroll', release, { passive: true })
    return () => scroller.removeEventListener('scroll', release)
  }, [enabled, minimumHeight, requiredScrollHeight])
  const { data, loading, error, retry } = useCredentialModelUsage(identityId, range, onAuthRequired, enabled, preserveScrollPosition, selection.revision)
  const models = data ? [...data.models].sort((a, b) => b.total_tokens - a.total_tokens) : []
  const totalName = t('usage_stats.credentials_models_total')
  return (
    <section ref={panelRef} className={styles.panel} data-credential-model-usage style={minimumHeight > 0 ? { minHeight: minimumHeight } : undefined}>
      <div className={styles.sectionHeading}>
        <h3>{t('usage_stats.credentials_models_title')}</h3>
        {data && models.length > 0 ? <span className={styles.modelCount}>{t('usage_stats.analysis_models_count', { count: models.length })}</span> : null}
        <div className={styles.rangeGroup} role="group" aria-label={t('usage_stats.credentials_models_range')}>
          {RANGES.map(option => (
            <button key={option} type="button" data-model-range={option} aria-pressed={(data?.range ?? range) === option}
              aria-busy={enabled && loading && range === option ? true : undefined}
              title={enabled && loading && range === option ? t('common.loading') : undefined}
              onClick={() => setSelection(current => current.range === option ? current : { range: option, revision: current.revision + 1 })}>
              {option === 'today' ? t('usage_stats.range_today') : t('usage_stats.range_last_days', { count: option === '30d' ? 30 : 7 })}
            </button>
          ))}
        </div>
      </div>
      {data && models.length > 0 ? <div className={styles.legendRow}><TokenLegend /></div> : null}
      {!data && loading ? <div className={styles.state} role="status">{t('common.loading')}</div> : null}
      {error ? <div className={styles.state} role="status">
        <span>{t(error === 'costs_busy' ? 'usage_stats.costs_busy' : 'usage_stats.credentials_models_load_failed')}</span>
        <Button type="button" variant="secondary" size="sm" onClick={retry} disabled={loading}>{t('common.retry')}</Button>
      </div> : null}
      {data ? models.length === 0 ? <div className={styles.state}>{t('usage_stats.credentials_models_empty')}</div>
            : <>
              <table className={styles.modelsTable}>
                <thead><tr>
                  <th scope="col">{t('usage_stats.credentials_models_model')}</th>
                  <th scope="col">{t('usage_stats.credentials_metric_requests')}</th>
                  <th scope="col">{t('usage_stats.credentials_models_token_composition')}</th>
                  <th scope="col">{t('usage_stats.cache_rate')}</th>
                  <th scope="col">{t('usage_stats.credentials_models_cost')}</th>
                </tr></thead>
                <tbody>{models.map(model => {
                  const name = model.model.trim() || '—'
                  const share = data.summary.total_tokens > 0 ? t('usage_stats.credentials_models_share', { percent: (model.total_tokens / data.summary.total_tokens * 100).toFixed(1) }) : '—'
                  return <tr key={model.model} className={styles.modelRow}>
                    <th scope="row"><ModelName text={name} enabled={enabled && !loading} /></th>
                    <td className={styles.requestsCell}><span className={styles.mobileLabel}>{t('usage_stats.credentials_metric_requests')} </span>{formatCredentialNumber(model.request_count)}</td>
                    <td className={styles.tokensCell}>
                      <div className={styles.tokenComposition}>
                        <strong>{formatCompactTokenValue(model.total_tokens)}</strong>
                        <TokenBreakdown model={model} name={name} enabled={enabled && !loading} />
                        <span><span className={styles.visuallyHidden}>{t('usage_stats.analysis_top_models_share')}: {share}</span><span aria-hidden="true">{share}</span></span>
                      </div>
                    </td>
                    <td className={styles.cacheCell}><span className={styles.mobileLabel}>{t('usage_stats.cache_rate')} </span><ModelCacheRate totals={model} /></td>
                    <td className={styles.costCell}><span className={styles.mobileLabel}>{t('usage_stats.credentials_models_cost')} </span><ModelCost totals={model} /></td>
                  </tr>
                })}</tbody>
                <tfoot><tr><th scope="row">{totalName}</th>
                  <td><span className={styles.mobileLabel}>{t('usage_stats.credentials_metric_requests')} </span>{formatCredentialNumber(data.summary.request_count)}</td>
                  <td>{isMobile ? <><span className={styles.mobileLabel}>Token </span><span className={styles.footerTokenTotal}><span>{formatCompactTokenValue(data.summary.total_tokens)}</span></span></> :
                    <div className={`${styles.tokenComposition} ${styles.footerComposition}`}>
                      <span className={styles.footerTokenTotal}><span>{formatCompactTokenValue(data.summary.total_tokens)}</span></span>
                      <TokenBreakdown model={data.summary} name={totalName} enabled={enabled && !loading} />
                      <span aria-hidden="true" />
                    </div>}
                  </td>
                  <td><span className={styles.mobileLabel}>{t('usage_stats.cache_rate')} </span><ModelCacheRate totals={data.summary} /></td>
                  <td><span className={styles.mobileLabel}>{t('usage_stats.credentials_models_cost')} </span><ModelCost totals={data.summary} /></td>
                </tr>{isMobile ? <tr className={styles.summaryBreakdownRow}><td colSpan={5}><TokenBreakdown model={data.summary} name={totalName} enabled={enabled && !loading} /></td></tr> : null}</tfoot>
              </table>
            </> : null}
    </section>
  )
}

function ModelName({ text, enabled }: { text: string; enabled: boolean }) {
  const anchor = useRef<HTMLElement>(null)
  const pointerType = useRef<string | null>(null)
  const [overflowing, setOverflowing] = useState(false)
  const id = `model-name-${useId()}`
  const { tooltip, showOnMouseEnter, hideOnMouseLeave, showOnFocus, hideOnBlur, dismiss } = usePortalTooltip({ trackAnchor: true })
  const interactive = enabled && overflowing
  const measureOverflow = useCallback(() => {
    const node = anchor.current
    if (!node) return
    const next = enabled && node.scrollWidth > node.clientWidth
    if (!next) dismiss()
    setOverflowing(current => current === next ? current : next)
  }, [dismiss, enabled])

  useLayoutEffect(() => {
    const node = anchor.current
    if (!node) return
    const frame = window.requestAnimationFrame(measureOverflow)
    const observer = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(measureOverflow)
    observer?.observe(node)
    window.addEventListener('resize', measureOverflow)
    document.fonts?.addEventListener('loadingdone', measureOverflow)
    return () => {
      window.cancelAnimationFrame(frame)
      observer?.disconnect()
      window.removeEventListener('resize', measureOverflow)
      document.fonts?.removeEventListener('loadingdone', measureOverflow)
    }
  }, [measureOverflow, text])
  useEffect(() => {
    if (!enabled) dismiss()
  }, [enabled, dismiss])
  useEffect(() => {
    if (!interactive || !tooltip) return
    const outside = (event: PointerEvent) => { if (!anchor.current?.contains(event.target as Node)) dismiss() }
    const escape = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && dismiss()) { event.preventDefault(); event.stopPropagation() }
    }
    document.addEventListener('pointerdown', outside, true)
    document.addEventListener('keydown', escape, true)
    return () => { document.removeEventListener('pointerdown', outside, true); document.removeEventListener('keydown', escape, true) }
  }, [interactive, tooltip, dismiss])

  return <>
    <strong ref={anchor} className={styles.modelName} data-model-name="model"
      tabIndex={interactive ? 0 : undefined} aria-describedby={interactive && tooltip ? id : undefined}
      onPointerDown={event => { pointerType.current = event.pointerType }}
      onPointerEnter={event => { if (interactive && event.pointerType !== 'touch') showOnMouseEnter([text], event.currentTarget) }}
      onPointerLeave={event => { if (event.pointerType !== 'touch') hideOnMouseLeave(event.currentTarget) }}
      onFocus={event => { if (interactive && !pointerType.current) showOnFocus([text], event.currentTarget) }}
      onBlur={event => { pointerType.current = null; hideOnBlur(event.currentTarget) }}
      onClick={event => { if (interactive && (pointerType.current === 'touch' || pointerType.current === 'pen')) { if (tooltip) dismiss(); else showOnFocus([text], event.currentTarget) } }}>
      {text}
    </strong>
    <PortalTooltip id={id} tooltip={interactive ? tooltip : null} />
  </>
}

function ModelCacheRate({ totals }: { totals: UsageIdentityModelTotals }) {
  const rate = calculateCacheReadRate({ inputTokens: totals.input_tokens, cacheReadTokens: totals.cache_read_tokens })
  return <span data-model-cache-rate className={credentialToneClassName('credentialMetricValue', cacheReadRateTone(rate))}>{formatCredentialPercent(rate)}</span>
}

function ModelCost({ totals }: { totals: UsageIdentityModelTotals }) {
  const { t } = useTranslation()
  return <>{formatUsd(totals.cost_usd)}{!totals.cost_available ? <abbr className={styles.costIncomplete} title={t('usage_stats.credentials_models_cost_incomplete')} aria-label={t('usage_stats.credentials_models_cost_incomplete')}>*</abbr> : null}</>
}

function TokenLegend() {
  const { t } = useTranslation()
  return <span className={styles.tokenLegend}>{TOKEN_PARTS.map(part => <span key={part.key}><i style={{ background: part.color }} />{t(part.label)}</span>)}</span>
}

function TokenBreakdown({ model, name, enabled }: { model: UsageIdentityModelTotals; name: string; enabled: boolean }) {
  const { t } = useTranslation()
  const id = `model-token-${useId()}`
  const anchor = useRef<HTMLDivElement>(null)
  const pointerType = useRef<string | null>(null)
  const { tooltip, showOnMouseEnter, showOnFocus, hideOnBlur, dismiss } = usePortalTooltip({ trackAnchor: true })
  const isOpen = tooltip !== null
  const totalLabel = t('usage_stats.total_tokens')
  const totalValue = formatCompactTokenValue(model.total_tokens)
  const parts = TOKEN_PARTS.map(part => ({ ...part, tooltipText: t(part.tooltipLabel), rawValue: formatCompactTokenValue(model[part.rawField]) }))
  const lines = [name, `${totalLabel}: ${totalValue}`, ...parts.map(part => `${part.tooltipText}: ${part.rawValue}`)]
  const values = {
    input: calculateDisplayInputTokens({ inputTokens: model.input_tokens, cacheReadTokens: model.cache_read_tokens, cacheCreationTokens: model.cache_creation_tokens }),
    cacheRead: model.cache_read_tokens,
    cacheCreation: model.cache_creation_tokens,
    output: calculateDisplayOutputTokens({ outputTokens: model.output_tokens, reasoningTokens: model.reasoning_tokens }),
    reasoning: model.reasoning_tokens,
  }
  useEffect(() => {
    if (!enabled) dismiss()
  }, [enabled, dismiss])
  useEffect(() => {
    if (!isOpen || !enabled) return
    const outside = (event: PointerEvent) => { if (!anchor.current?.contains(event.target as Node)) dismiss() }
    const escape = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && dismiss()) { event.preventDefault(); event.stopPropagation() }
    }
    document.addEventListener('pointerdown', outside, true)
    document.addEventListener('keydown', escape, true)
    return () => { document.removeEventListener('pointerdown', outside, true); document.removeEventListener('keydown', escape, true) }
  }, [isOpen, enabled, dismiss])

  return <>
    <div ref={anchor} className={styles.tokenTarget} data-token-breakdown role="img" tabIndex={enabled ? 0 : -1}
      aria-label={lines.join('; ')} aria-describedby={tooltip ? id : undefined}
      onPointerDown={event => { pointerType.current = event.pointerType }}
      onPointerEnter={event => { if (enabled && event.pointerType !== 'touch') showOnMouseEnter(lines, event.currentTarget) }}
      onPointerLeave={event => { if (event.pointerType !== 'touch') dismiss() }}
      onFocus={event => { if (enabled && !pointerType.current) showOnFocus(lines, event.currentTarget) }}
      onBlur={event => { pointerType.current = null; hideOnBlur(event.currentTarget) }}
      onClick={event => { if (enabled && (pointerType.current === 'touch' || pointerType.current === 'pen')) { if (isOpen) dismiss(); else showOnFocus(lines, event.currentTarget) } }}>
      <div className={styles.tokenTrack}>
        {TOKEN_PARTS.map(part => <span key={part.key} data-token-part={part.key} data-token-value={values[part.key]} style={{ width: `${model.total_tokens > 0 ? values[part.key] / model.total_tokens * 100 : 0}%`, background: part.color }} />)}
      </div>
    </div>
    <PortalTooltip id={id} tooltip={enabled && tooltip ? { ...tooltip, lines } : null}>
      <div className={styles.tokenTooltip}>
        <strong className={styles.tokenTooltipTitle}>{name}</strong>
        <div className={styles.tokenTooltipRow}><span aria-hidden="true" /><span>{totalLabel}: </span><span>{totalValue}</span></div>
        {parts.map(part => <div key={part.key} className={styles.tokenTooltipRow}>
          <i className={styles.tokenTooltipDot} data-token-tooltip-part={part.key} aria-hidden="true" style={{ background: part.color }} />
          <span>{part.tooltipText}: </span><span>{part.rawValue}</span>
        </div>)}
      </div>
    </PortalTooltip>
  </>
}
