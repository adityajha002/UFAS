// Per-day status for the history chips (APP-3). Pure, no React.
import type { HistoryResponse } from '../api/client';
import { colors } from '../theme/colors';
import { toCampusDate, ymd } from './dates';
import { formatWorked } from './inOut';

export type DayStatus = 'COMPLETE' | 'IN_PROGRESS' | 'INCOMPLETE' | 'NONE';
export type DayDetail = { status: DayStatus; workedS: number | null };

const SERVER_STATUSES = new Set<DayStatus>(['COMPLETE', 'IN_PROGRESS', 'INCOMPLETE']);
const DATE_RE = /^\d{4}-\d{2}-\d{2}$/;

function entry(status: unknown, worked: unknown): DayDetail | null {
  if (typeof status !== 'string' || !SERVER_STATUSES.has(status as DayStatus)) return null;
  const workedS = typeof worked === 'number' && Number.isFinite(worked) && worked >= 0 ? worked : null;
  return { status: status as DayStatus, workedS };
}

/**
 * Normalises the server's `day_status` into date -> detail.
 * PROVISIONAL: the exact shape is in 00_OVERVIEW.md §5.2, which is not in the repo yet, so this
 * accepts the likely forms and skips anything it does not understand:
 *   [{ day, status, worked_s? }]   { "YYYY-MM-DD": "STATUS" }   { "YYYY-MM-DD": { status, worked_s? } }
 */
export function parseDayStatus(raw: unknown): Map<string, DayDetail> {
  const out = new Map<string, DayDetail>();
  if (Array.isArray(raw)) {
    for (const item of raw) {
      const o = item as { day?: unknown; status?: unknown; worked_s?: unknown } | null;
      const d = o && typeof o.day === 'string' && DATE_RE.test(o.day) ? entry(o.status, o.worked_s) : null;
      if (d) out.set(o!.day as string, d);
    }
  } else if (raw && typeof raw === 'object') {
    for (const [day, v] of Object.entries(raw as Record<string, unknown>)) {
      if (!DATE_RE.test(day)) continue;
      const d = typeof v === 'string'
        ? entry(v, null)
        : entry((v as { status?: unknown } | null)?.status, (v as { worked_s?: unknown } | null)?.worked_s);
      if (d) out.set(day, d);
    }
  }
  return out;
}

/**
 * Status of each date as the server last reported it:
 *  - a `day_status` entry wins; IN_PROGRESS is amber only on the server's `today` (an earlier day
 *    still IN_PROGRESS is shown INCOMPLETE, the server's own effective rule)
 *  - otherwise a date in `days` is COMPLETE with no hours (older server, legacy rows, old cache)
 *  - otherwise NONE
 */
export function dayDetailLookup(data: HistoryResponse | null, nowMs: number): (date: string) => DayDetail {
  const statuses = parseDayStatus(data?.day_status);
  const marked = new Set(data?.days ?? []);
  const today = data?.today && DATE_RE.test(data.today) ? data.today : ymd(toCampusDate(nowMs));
  return (date) => {
    const s = statuses.get(date);
    if (s) {
      if (s.status === 'IN_PROGRESS' && date < today) return { status: 'INCOMPLETE', workedS: s.workedS };
      if (s.status === 'IN_PROGRESS' && date === today && s.workedS === null && typeof data?.today_worked_s === 'number') {
        return { status: s.status, workedS: data.today_worked_s };
      }
      return s;
    }
    return { status: marked.has(date) ? 'COMPLETE' : 'NONE', workedS: null };
  };
}

/** True when the server sends per-day statuses (so the legend means something). */
export const hasDayStatus = (data: HistoryResponse | null): boolean => parseDayStatus(data?.day_status).size > 0;

export const DAY_STYLE: Record<DayStatus, { bg: string; mark: string; word: string }> = {
  COMPLETE: { bg: colors.green, mark: '✓ ', word: 'complete' },
  IN_PROGRESS: { bg: colors.amber, mark: '• ', word: 'in progress' },
  INCOMPLETE: { bg: colors.red, mark: '! ', word: 'incomplete' },
  NONE: { bg: colors.chipGrey, mark: '', word: 'no record' },
};

/** Hours to show for a day, or null (legacy day, no record, or nothing worked yet today). */
export function workedText(d: DayDetail): string | null {
  if (d.workedS === null || d.status === 'NONE') return null;
  if (d.status === 'IN_PROGRESS' && d.workedS === 0) return null;
  return formatWorked(d.workedS);
}

const FULL: Record<string, string> = { MON: 'MONDAY', TUES: 'TUESDAY', WEDS: 'WEDNESDAY', THU: 'THURSDAY', FRI: 'FRIDAY' };

/** Line shown when a chip is tapped, e.g. "TUESDAY 10/11 · COMPLETE · 6h 05m". */
export function dayDetailText(label: string, date: string, d: DayDetail): string {
  const [, m, day] = date.split('-');
  const parts = [`${FULL[label] ?? label} ${Number(day)}/${m}`, DAY_STYLE[d.status].word.toUpperCase()];
  const hours = workedText(d);
  if (hours) parts.push(hours);
  return parts.join(' · ');
}
