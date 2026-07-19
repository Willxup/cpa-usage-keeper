import type { HTMLAttributes, ReactNode } from 'react';
import styles from './SectionHeader.module.scss';

const joinClassNames = (...classNames: Array<string | false | null | undefined>) => (
  classNames.filter(Boolean).join(' ')
);

export type SectionHeaderLevel = 2 | 3;
export type SectionHeaderSize = 'section' | 'subsection';

export type SectionHeaderProps = Omit<HTMLAttributes<HTMLElement>, 'title'> & {
  title: ReactNode;
  description?: ReactNode;
  meta?: ReactNode;
  actions?: ReactNode;
  headingLevel?: SectionHeaderLevel;
  size?: SectionHeaderSize;
};

export function SectionHeader({
  title,
  description,
  meta,
  actions,
  headingLevel = 2,
  size = headingLevel === 2 ? 'section' : 'subsection',
  className,
  ...props
}: SectionHeaderProps) {
  const Heading = headingLevel === 2 ? 'h2' : 'h3';

  return (
    <header
      {...props}
      className={joinClassNames(
        styles.sectionHeader,
        size === 'section' ? styles.section : styles.subsection,
        className,
      )}
    >
      <div className={styles.copy}>
        <div className={styles.titleRow}>
          <Heading className={styles.title}>{title}</Heading>
          {meta && <div className={styles.meta}>{meta}</div>}
        </div>
        {description && <div className={styles.description}>{description}</div>}
      </div>
      {actions && <div className={styles.actions}>{actions}</div>}
    </header>
  );
}
