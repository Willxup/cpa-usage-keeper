export const DEFAULT_TIME_ZONE = 'America/Chicago';
export const TIME_ZONE_STORAGE_KEY = 'cpa-usage-keeper-time-zone-v1';
export const TIME_ZONE_CHANGE_EVENT = 'cpa-usage-keeper-time-zone-change';

export const isValidTimeZone = (value: unknown): value is string => {
  if (typeof value !== 'string' || !value.trim()) return false;
  try { new Intl.DateTimeFormat('en-US', { timeZone: value }).format(); return true; } catch { return false; }
};

export const getBrowserTimeZone = (): string => {
  try { return isValidTimeZone(Intl.DateTimeFormat().resolvedOptions().timeZone) ? Intl.DateTimeFormat().resolvedOptions().timeZone : DEFAULT_TIME_ZONE; } catch { return DEFAULT_TIME_ZONE; }
};

export type TimeZonePreference = 'auto' | string;
export const getTimeZonePreference = (): TimeZonePreference => {
  if (typeof window === 'undefined') return 'auto';
  const value = window.localStorage.getItem(TIME_ZONE_STORAGE_KEY);
  return value === 'auto' || isValidTimeZone(value) ? (value ?? 'auto') : 'auto';
};
export const getDisplayTimeZone = (preference = getTimeZonePreference()): string => preference === 'auto' ? getBrowserTimeZone() : (isValidTimeZone(preference) ? preference : DEFAULT_TIME_ZONE);
export const formatDateTime = (value: string | number | Date, options: Intl.DateTimeFormatOptions = {}): string => {
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) return '';
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short', timeZone: getDisplayTimeZone(), ...options }).format(date);
};
export const saveTimeZonePreference = (value: TimeZonePreference): void => {
  if (typeof window === 'undefined') return;
  window.localStorage.setItem(TIME_ZONE_STORAGE_KEY, value);
  window.dispatchEvent(new CustomEvent(TIME_ZONE_CHANGE_EVENT));
};
