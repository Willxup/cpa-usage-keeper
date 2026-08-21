import { useEffect, useState } from 'react';
import { getBrowserTimeZone, getDisplayTimeZone, getTimeZonePreference, saveTimeZonePreference, TIME_ZONE_CHANGE_EVENT } from '@/utils/timezone';

const COMMON = ['America/Chicago', 'America/Los_Angeles', 'America/New_York', 'Europe/London', 'Europe/Berlin', 'Asia/Shanghai', 'Asia/Tokyo', 'Australia/Sydney'];
export function TimezoneSettings() {
  const [preference, setPreference] = useState(getTimeZonePreference);
  useEffect(() => { const onChange = () => setPreference(getTimeZonePreference()); window.addEventListener(TIME_ZONE_CHANGE_EVENT, onChange); return () => window.removeEventListener(TIME_ZONE_CHANGE_EVENT, onChange); }, []);
  const change = (value: string) => { setPreference(value); saveTimeZonePreference(value); };
  return <label className="app-timezone-setting">Time zone: <select value={preference} onChange={(event) => change(event.target.value)}><option value="auto">Automatic ({getBrowserTimeZone()})</option>{COMMON.map((zone) => <option key={zone} value={zone}>{zone}</option>)}</select><small>Displaying {getDisplayTimeZone(preference)}</small></label>;
}
