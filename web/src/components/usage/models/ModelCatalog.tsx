import { forwardRef, useEffect, useImperativeHandle, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ProviderBrandIcon } from '@/components/ProviderBrandIcon'
import { Button } from '@/components/ui/Button'
import { SearchInput } from '@/components/ui/SearchInput'
import { IconCopy, IconFilterAll, IconSearch } from '@/components/ui/icons'
import { copyTextToClipboard } from '@/utils/clipboard'
import { loadModelProviderFilter, modelProviderFromFilter, persistModelProviderFilter, type ModelProvider } from './modelCatalog'
import type { ModelCatalogData } from './useModelCatalogData'
import filterStyles from '../credentials/CredentialSections.module.scss'
import styles from './ModelCatalog.module.scss'

export interface ModelCatalogHandle { refresh: () => Promise<void> }

export const ModelCatalog = forwardRef<ModelCatalogHandle, { catalog: ModelCatalogData }>(function ModelCatalog({ catalog }, ref) {
  const { t } = useTranslation()
  const { models, loading, loaded, error, refresh } = catalog
  const [provider, setProvider] = useState(loadModelProviderFilter)
  const [search, setSearch] = useState('')
  const [copyNotice, setCopyNotice] = useState('')
  const listViewport = useRef<HTMLDivElement | null>(null)
  const searchInput = useRef<HTMLInputElement>(null)

  useImperativeHandle(ref, () => ({ refresh }), [refresh])
  useEffect(() => {
    if (!copyNotice) return
    const timer = setTimeout(() => setCopyNotice(''), 2500)
    return () => clearTimeout(timer)
  }, [copyNotice])

  const providers = useMemo(() => {
    const groups = new Map<string, ModelProvider & { count: number }>()
    for (const model of models) {
      if (!model.provider.icon) continue
      const existing = groups.get(model.provider.key)
      groups.set(model.provider.key, { ...model.provider, count: (existing?.count ?? 0) + 1 })
    }
    // 有图标的筛选暂时没有结果也保留选择；无图标的模型只计入 All。
    const selected = modelProviderFromFilter(provider)
    if (selected && !groups.has(selected.key)) groups.set(selected.key, { ...selected, count: 0 })
    return [...groups.values()].sort((a, b) => a.label.localeCompare(b.label))
  }, [models, provider])
  const filtered = useMemo(() => {
    const query = search.trim().toLowerCase()
    return models.filter((model) => (provider === 'all' || model.provider.key === provider) &&
      (!query || `${model.id} ${model.provider.icon ? model.provider.label : ''}`.toLowerCase().includes(query)))
  }, [models, provider, search])
  const resetScroll = () => { if (listViewport.current) listViewport.current.scrollTop = 0 }
  const selectProvider = (value: string) => { setProvider(value); persistModelProviderFilter(value); resetScroll() }
  const copy = async (id: string) => {
    try { await copyTextToClipboard(id); setCopyNotice(t('usage_stats.models_copied', { model: id })) }
    catch { setCopyNotice(t('usage_stats.models_copy_failed')) }
  }

  return <section className={styles.catalog} aria-label={t('usage_stats.models_catalog_title')}>
    <h2 className={styles.title}>{t('usage_stats.models_catalog_title')}</h2>
    <div className={filterStyles.credentialProviderFilterBar} role="toolbar" aria-label={t('usage_stats.models_filter_label')}>
      {[{ key: 'all', label: t('usage_stats.models_all'), count: models.length }, ...providers].map((item) => <button
        key={item.key} type="button" aria-pressed={provider === item.key}
        className={`${filterStyles.credentialProviderFilterButton} ${provider === item.key ? filterStyles.credentialProviderFilterButtonActive : ''}`}
        onClick={() => selectProvider(item.key)}>
        <span className={filterStyles.credentialProviderFilterIconFrame}>
          {item.key === 'all' ? <IconFilterAll size={30} /> : 'icon' in item && item.icon
            ? <ProviderBrandIcon providerType={item.icon} size="100%" /> : null}
        </span>
        <span className={filterStyles.credentialProviderFilterLabel}>{item.label}</span>
        <span className={filterStyles.credentialProviderFilterCount}>{loaded ? item.count : '—'}</span>
      </button>)}
    </div>
    <div className={styles.tools}>
      <SearchInput ref={searchInput} value={search} placeholder={t('usage_stats.models_search')}
        onChange={(value) => { setSearch(value); resetScroll() }} />
    </div>
    {error ? <div className={styles.error} role="alert">
      <span>{t(loaded ? 'usage_stats.models_refresh_failed' : 'usage_stats.models_load_failed')}</span>
      <Button variant="secondary" appearance="action" disabled={loading} onClick={() => void refresh()}>{t('common.retry')}</Button>
    </div> : null}
    <div ref={listViewport} className={styles.listViewport} aria-busy={loading}
      role="region" aria-label={t('usage_stats.models_list_label')} tabIndex={0}>
      {loading && !loaded ? <div className={styles.grid} aria-label={t('common.loading')}>
        {Array.from({ length: 12 }, (_, index) => <div key={index} className={styles.skeleton} />)}
      </div> : filtered.length ? <div className={styles.grid}>
        {filtered.map((model) => <article key={model.id} className={styles.model}>
          <div className={styles.modelTop}>
            {model.provider.icon ? <ProviderBrandIcon providerType={model.provider.icon} size={28} />
              : <span className={styles.unknownIcon} aria-hidden="true">{model.provider.label.slice(0, 1).toUpperCase() || '?'}</span>}
            <span className={styles.owner}>{model.provider.label}</span>
            <button type="button" className={styles.copy} aria-label={t('usage_stats.models_copy', { model: model.id })}
              title={t('usage_stats.models_copy', { model: model.id })} onClick={() => void copy(model.id)}><IconCopy size={16} /></button>
          </div>
          <h3 title={model.id}>{model.id}</h3>
        </article>)}
      </div> : !error || loaded ? <div className={styles.empty}>
        <IconSearch size={26} />
        <strong>{t(models.length ? 'usage_stats.models_no_matches' : 'usage_stats.models_empty')}</strong>
        <span>{t('usage_stats.models_empty_hint')}</span>
        {provider !== 'all' || search ? <Button variant="secondary" appearance="action" onClick={() => { selectProvider('all'); setSearch(''); searchInput.current?.focus() }}>{t('usage_stats.models_clear_filters')}</Button> : null}
      </div> : null}
    </div>
    {copyNotice ? <span className={styles.copyNotice} role="status">{copyNotice}</span> : null}
  </section>
})
