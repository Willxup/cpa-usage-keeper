import { forwardRef, type ButtonHTMLAttributes } from 'react'
import styles from './QuestionMarkHelpButton.module.scss'

type QuestionMarkHelpButtonProps = Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'children' | 'type'>

export const QuestionMarkHelpButton = forwardRef<HTMLButtonElement, QuestionMarkHelpButtonProps>(
  function QuestionMarkHelpButton({ className, ...props }, ref) {
    return (
      <button
        {...props}
        ref={ref}
        type="button"
        className={`${styles.button} ${className ?? ''}`.trim()}
        data-question-mark-help="true"
      >
        ?
      </button>
    )
  },
)
