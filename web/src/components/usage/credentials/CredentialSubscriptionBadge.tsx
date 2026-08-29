import { useTranslation } from 'react-i18next'
import styles from './CredentialSections.module.scss'
import type { SubscriptionBadgeKind, SubscriptionBadgeModel } from './credentialSubscription'

type BadgePresentation = {
  className: string
  hasPremiumMotion: boolean
}

const BADGE_PRESENTATIONS: Record<SubscriptionBadgeKind, BadgePresentation> = {
  'codex-free': { className: styles.credentialPlanBadgeFree, hasPremiumMotion: false },
  'codex-plus': { className: styles.credentialPlanBadgePlus, hasPremiumMotion: true },
  'codex-team': { className: styles.credentialPlanBadgeTeam, hasPremiumMotion: true },
  'codex-pro5x': { className: styles.credentialPlanBadgePro5x, hasPremiumMotion: true },
  'codex-pro20x': { className: styles.credentialPlanBadgePro20x, hasPremiumMotion: true },
  'codex-enterprise': { className: styles.credentialPlanBadgeEnterprise, hasPremiumMotion: true },
  'codex-unknown': { className: styles.credentialPlanBadgeNeutral, hasPremiumMotion: false },
  'claude-free': { className: styles.credentialPlanBadgeFree, hasPremiumMotion: false },
  'claude-pro': { className: styles.credentialPlanBadgePlus, hasPremiumMotion: true },
  'claude-max': { className: styles.credentialPlanBadgePro20x, hasPremiumMotion: true },
  'claude-team': { className: styles.credentialPlanBadgeTeam, hasPremiumMotion: true },
  'antigravity-free': { className: styles.credentialPlanBadgeFree, hasPremiumMotion: false },
  'antigravity-pro': { className: styles.credentialPlanBadgePlus, hasPremiumMotion: true },
  'antigravity-ultra-lite': { className: styles.credentialPlanBadgePro5x, hasPremiumMotion: true },
  'antigravity-ultra': { className: styles.credentialPlanBadgePro20x, hasPremiumMotion: true },
  'antigravity-unknown': { className: styles.credentialPlanBadgeNeutral, hasPremiumMotion: false },
}

export function CredentialSubscriptionBadge({ model }: { model: SubscriptionBadgeModel }) {
  const { t } = useTranslation()
  const presentation = BADGE_PRESENTATIONS[model.kind]
  const label = model.labelKey ? t(model.labelKey) : model.fallbackLabel
  if (!label) {
    return null
  }

  return (
    <span className={`${styles.credentialPlanBadge} ${presentation.className}`.trim()}>
      {/* 所有高级套餐复用同一组 transform 动画层，provider 只选择既有 presentation。 */}
      {presentation.hasPremiumMotion && <span className={styles.credentialPlanBadgeFlow} aria-hidden="true" />}
      {presentation.hasPremiumMotion && <span className={styles.credentialPlanBadgeCorona} aria-hidden="true" />}
      <span className={styles.credentialPlanBadgeLabel}>{label}</span>
    </span>
  )
}
