import { IconSearch } from './icons'
import styles from './SearchInput.module.scss'

interface SearchInputProps {
  value: string
  placeholder: string
  onChange: (value: string) => void
}

export function SearchInput({ value, placeholder, onChange }: SearchInputProps) {
  return <label className={styles.search}>
    <IconSearch size={17} />
    <input type="search" value={value} placeholder={placeholder} aria-label={placeholder}
      onChange={(event) => onChange(event.target.value)} />
  </label>
}
