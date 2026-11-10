import { addDays } from './dates';
const MONTHS = ['JAN', 'FEB', 'MAR', 'APR', 'MAY', 'JUNE', 'JULY', 'AUG', 'SEPT', 'OCT', 'NOV', 'DEC'];
const SUFFIX = ['ST', 'ND', 'RD', 'TH', 'TH'];
const fmt = (d: Date) =>
  `${d.getUTCDate()}/${String(d.getUTCMonth() + 1).padStart(2, '0')}/${String(d.getUTCFullYear()).slice(-2)}`;

/** "d/mm/yy - d/mm/yy", Monday to Sunday. */
export const weekRange = (monday: Date) => `${fmt(monday)} - ${fmt(addDays(monday, 6))}`;

/** Month and week number come from the week's Thursday (which Thursday of the month it is). */
export function weekCaption(monday: Date) {
  const thu = addDays(monday, 3);
  const idx = Math.ceil(thu.getUTCDate() / 7) - 1;
  return { month: MONTHS[thu.getUTCMonth()], n: String(idx + 1), suffix: SUFFIX[idx] };
}

export const weekCaptionText = (monday: Date) => {
  const c = weekCaption(monday);
  return `${c.month} - ${c.n}${c.suffix} WEEK`;
};
