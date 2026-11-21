import { useCallback, useEffect, useId, useLayoutEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/Button'
import { PortalTooltip, usePortalTooltip } from '@/components/ui/PortalTooltip'
import type { UsageIdentityModel, UsageIdentityModelsRange, UsageIdentityModelTotals } from '@/lib/types'
import { calculateDisplayInputTokens, calculateDisplayOutputTokens, formatCompactTokenValue, formatUsd } from '@/utils/usage'
import { USAGE_CHART_TOKEN_COLORS } from '@/utils/usage/chartConfig'
import { formatCredentialNumber } from './CredentialSectionShell'
import { useCredentialModelUsage } from './useCredentialModelUsage'
import styles from './CredentialModelUsage.module.scss'

const RANGES: UsageIdentityModelsRange[] = ['today', '7d', '30d']
const TOKEN_PARTS = [
  { key: 'input', label: 'usage_stats.credentials_models_input', color: USAGE_CHART_TOKEN_COLORS.input.base },
  { key: 'cacheRead', label: 'usage_stats.credentials_models_cache_read', color: USAGE_CHART_TOKEN_COLORS.cacheRead.base },
  { key: 'cacheCreation', label: 'usage_stats.credentials_models_cache_write', color: USAGE_CHART_TOKEN_COLORS.cacheWrite.base },
  { key: 'output', label: 'usage_stats.credentials_models_output', color: USAGE_CHART_TOKEN_COLORS.output.base },
  { key: 'reasoning', label: 'usage_stats.credentials_models_reasoning', color: USAGE_CHART_TOKEN_COLORS.reasoning.base },
] as const

interface CredentialModelUsageProps {
  identityId: string
  kind: 'credential' | 'ai-provider'
  onAuthRequired?: () => void
  enabled?: boolean
}

export function CredentialModelUsage(props: CredentialModelUsageProps) {
  // 身份改变时重置范围，同时立即卸载旧列表及其 Portal tooltip。
  return <ModelUsage key={props.identityId} {...props} />
}

function ModelUsage({ identityId, kind, onAuthRequired, enabled = true }: CredentialModelUsageProps) {
  const { t } = useTranslation()
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
  return (
    <section ref={panelRef} className={styles.panel} data-credential-model-usage style={minimumHeight > 0 ? { minHeight: minimumHeight } : undefined}>
      <div className={styles.sectionHeading}>
        <h3>{t(kind === 'ai-provider' ? 'usage_stats.credentials_models_mapping_title' : 'usage_stats.credentials_models_title')}</h3>
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
                  <th scope="col">{t(kind === 'ai-provider' ? 'usage_stats.credentials_models_mapping_column' : 'usage_stats.credentials_models_model')}</th>
                  <th scope="col">{t('usage_stats.credentials_metric_requests')}</th>
                  <th scope="col">{t('usage_stats.credentials_models_token_composition')}</th>
                  <th scope="col">{t('usage_stats.credentials_models_cost')}</th>
                </tr></thead>
                <tbody>{models.map(model => {
                  const alias = model.model_alias.trim()
                  const name = alias || model.model || '—'
                  const share = data.summary.total_tokens > 0 ? t('usage_stats.credentials_models_share', { percent: (model.total_tokens / data.summary.total_tokens * 100).toFixed(1) }) : '—'
                  return <tr key={JSON.stringify([model.model, model.model_alias])} className={styles.modelRow}>
                    <th scope="row"><strong>{name}</strong>{kind === 'ai-provider' && alias && model.model && alias !== model.model ? <span className={styles.upstream}>→ {model.model}</span> : null}</th>
                    <td className={styles.requestsCell}><span className={styles.mobileLabel}>{t('usage_stats.credentials_metric_requests')} </span>{formatCredentialNumber(model.request_count)}</td>
                    <td className={styles.tokensCell}>
                      <div className={styles.tokenComposition}>
                        <strong>{formatCompactTokenValue(model.total_tokens)}</strong>
                        <TokenBreakdown model={model} name={name} enabled={enabled && !loading} />
                        <span><span className={styles.visuallyHidden}>{t('usage_stats.analysis_top_models_share')}: {share}</span><span aria-hidden="true">{share}</span></span>
                      </div>
                    </td>
                    <td className={styles.costCell}><span className={styles.mobileLabel}>{t('usage_stats.credentials_models_cost')} </span><ModelCost totals={model} /></td>
                  </tr>
                })}</tbody>
                <tfoot><tr><th scope="row">{t('usage_stats.credentials_models_total')}</th>
                  <td><span className={styles.mobileLabel}>{t('usage_stats.credentials_metric_requests')} </span>{formatCredentialNumber(data.summary.request_count)}</td>
                  <td><span className={styles.mobileLabel}>Token </span><span className={styles.footerTokenTotal}><span>{formatCompactTokenValue(data.summary.total_tokens)}</span></span></td>
                  <td><span className={styles.mobileLabel}>{t('usage_stats.credentials_models_cost')} </span><ModelCost totals={data.summary} /></td>
                </tr></tfoot>
              </table>
            </> : null}
    </section>
  )
}

function ModelCost({ totals }: { totals: UsageIdentityModelTotals }) {
  const { t } = useTranslation()
  return <>{formatUsd(totals.cost_usd)}{!totals.cost_available ? <abbr className={styles.costIncomplete} title={t('usage_stats.credentials_models_cost_incomplete')} aria-label={t('usage_stats.credentials_models_cost_incomplete')}>*</abbr> : null}</>
}

function TokenLegend() {
  const { t } = useTranslation()
  return <span className={styles.tokenLegend}>{TOKEN_PARTS.map(part => <span key={part.key}><i style={{ background: part.color }} />{t(part.label)}</span>)}</span>
}

function TokenBreakdown({ model, name, enabled }: { model: UsageIdentityModel; name: string; enabled: boolean }) {
  const { t } = useTranslation()
  const id = `model-token-${useId()}`
  const anchor = useRef<HTMLDivElement>(null)
  const pointerType = useRef<string | null>(null)
  const { tooltip, showOnMouseEnter, showOnFocus, hideOnBlur, dismiss } = usePortalTooltip({ trackAnchor: true })
  const isOpen = tooltip !== null
  const lines = [name,
    `${t('usage_stats.total_tokens')}: ${formatCompactTokenValue(model.total_tokens)}`,
    `${t('usage_stats.input_tokens')}: ${formatCompactTokenValue(model.input_tokens)}`,
    `${t('usage_stats.cache_read_tokens')}: ${formatCompactTokenValue(model.cache_read_tokens)}`,
    `${t('usage_stats.cache_creation_tokens')}: ${formatCompactTokenValue(model.cache_creation_tokens)}`,
    `${t('usage_stats.output_tokens')}: ${formatCompactTokenValue(model.output_tokens)}`,
    `${t('usage_stats.reasoning_tokens')}: ${formatCompactTokenValue(model.reasoning_tokens)}`,
  ]
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
    <PortalTooltip id={id} tooltip={enabled && tooltip ? { ...tooltip, lines } : null} />
  </>
}
