import { addDays, mondayOf, subtractMonths, toCampusDate, ymd } from './dates';
import type { DayDetail, DayStatus } from './dayStatus';
export type DayInfo = { label: string; date: string; status: DayStatus; workedS: number | null; isToday: boolean };
export type Week = { monday: Date; days: DayInfo[] };
const LABELS = ['MON', 'TUES', 'WEDS', 'THU', 'FRI'];
const MONTHS = ['JANUARY', 'FEBRUARY', 'MARCH', 'APRIL', 'MAY', 'JUNE', 'JULY', 'AUGUST', 'SEPTEMBER', 'OCTOBER', 'NOVEMBER', 'DECEMBER'];

/**
 * Monday-to-Friday weeks from the current week back to `monthsBack` months ago, newest first.
 * `detailOf` gives each campus date's status (see dayStatus.ts dayDetailLookup); weekends are not shown.
 */
export function groupByWeek(detailOf: (date: string) => DayDetail, nowMs: number, monthsBack = 2): Week[] {
  const today = toCampusDate(nowMs);
  const todayYmd = ymd(today);
  const first = mondayOf(subtractMonths(today, monthsBack));
  const weeks: Week[] = [];
  for (let m = mondayOf(today); m.getTime() >= first.getTime(); m = addDays(m, -7)) {
    weeks.push({
      monday: m,
      days: LABELS.map((label, i) => {
        const date = ymd(addDays(m, i));
        return { label, date, ...detailOf(date), isToday: date === todayYmd };
      }),
    });
  }
  return weeks;
}

export type MonthSummary = { month: string; complete: number; inProgress: number; incomplete: number };

/** Counts for the current campus month (weekdays shown in `weeks`). */
export function monthSummary(weeks: Week[], nowMs: number): MonthSummary {
  const today = toCampusDate(nowMs);
  const prefix = ymd(today).slice(0, 7); // YYYY-MM
  const out: MonthSummary = { month: MONTHS[today.getUTCMonth()], complete: 0, inProgress: 0, incomplete: 0 };
  for (const d of weeks.flatMap((w) => w.days)) {
    if (!d.date.startsWith(prefix)) continue;
    if (d.status === 'COMPLETE') out.complete++;
    else if (d.status === 'IN_PROGRESS') out.inProgress++;
    else if (d.status === 'INCOMPLETE') out.incomplete++;
  }
  return out;
}

/** "NOVEMBER · 6 COMPLETE · 1 INCOMPLETE" (zero counts other than complete are left out). */
export function monthSummaryText(m: MonthSummary): string {
  const parts = [m.month, `${m.complete} COMPLETE`];
  if (m.inProgress) parts.push(`${m.inProgress} IN PROGRESS`);
  if (m.incomplete) parts.push(`${m.incomplete} INCOMPLETE`);
  return parts.join(' · ');
}
