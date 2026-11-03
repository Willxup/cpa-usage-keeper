import { forwardRef } from 'react'
import { IconSearch } from './icons'
import styles from './SearchInput.module.scss'

interface SearchInputProps {
  value: string
  placeholder: string
  onChange: (value: string) => void
}

export const SearchInput = forwardRef<HTMLInputElement, SearchInputProps>(function SearchInput({ value, placeholder, onChange }, ref) {
  return <label className={styles.search}>
    <IconSearch size={17} />
    <input ref={ref} type="search" value={value} placeholder={placeholder} aria-label={placeholder}
      onChange={(event) => onChange(event.target.value)} />
  </label>
})
