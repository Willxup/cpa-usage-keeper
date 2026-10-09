import { useEffect, useState } from 'react';
import { createPortal } from 'react-dom';
import { useTranslation } from 'react-i18next';
import { IconChevronUp } from '@/components/ui/icons';
import { GlassSurface } from './GlassSurface';
import styles from './BackToTop.module.scss';

export function BackToTop() {
  const { t } = useTranslation();
  const [visible, setVisible] = useState(false);

  useEffect(() => {
    const update = () => setVisible(window.scrollY > 320);
    update();
    window.addEventListener('scroll', update, { passive: true });
    return () => window.removeEventListener('scroll', update);
  }, []);

  // 放到 body，避免工具条的 sticky/container 上下文改变按钮的固定位置。
  return createPortal(<button
    type="button"
    className={styles.button}
    data-dashboard-back-to-top
    data-visible={visible}
    disabled={!visible}
    aria-hidden={!visible}
    aria-label={t('usage_stats.back_to_top')}
    title={t('usage_stats.back_to_top')}
    onClick={() => window.scrollTo({ top: 0, behavior: window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'instant' : 'smooth' })}
  >
    <GlassSurface />
    <IconChevronUp size={20} aria-hidden="true" />
  </button>, document.body);
}
