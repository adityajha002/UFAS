// All date maths is done on "YYYY-MM-DD" strings through Date.UTC, so the
// browser's timezone can never shift a date. "Today" always comes from the
// server (status.server_time_ist); this file never reads the system clock.

const RE = /^(\d{4})-(\d{2})-(\d{2})$/;
const DAY_MS = 86_400_000;
const DOW = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
const MON = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

function toUtc(s: string): number {
  const m = RE.exec(s);
  if (!m) throw new Error(`bad date: ${s}`);
  return Date.UTC(+m[1], +m[2] - 1, +m[3]);
}
function fromUtc(ms: number): string {
  const d = new Date(ms);
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getUTCFullYear()}-${p(d.getUTCMonth() + 1)}-${p(d.getUTCDate())}`;
}

export function isYmd(s: string): boolean {
  if (!RE.test(s)) return false;
  return fromUtc(toUtc(s)) === s; // rejects 2026-02-30
}
export const addDays = (s: string, n: number) => fromUtc(toUtc(s) + n * DAY_MS);
export const diffDays = (a: string, b: string) => Math.round((toUtc(b) - toUtc(a)) / DAY_MS);
export const firstOfMonth = (s: string) => `${s.slice(0, 7)}-01`;

/** "Fri 30 Oct 2026" */
export function fmtDay(s: string): string {
  const d = new Date(toUtc(s));
  return `${DOW[d.getUTCDay()]} ${d.getUTCDate()} ${MON[d.getUTCMonth()]} ${d.getUTCFullYear()}`;
}
/** "30 Oct" */
export function fmtShort(s: string): string {
  const d = new Date(toUtc(s));
  return `${d.getUTCDate()} ${MON[d.getUTCMonth()]}`;
}
/** "2026-10-30 09:41:07" -> "09:41:07" */
export const timePart = (ist: string) => ist.slice(11, 19);
export const datePart = (ist: string) => ist.slice(0, 10);

export function fmtHours(seconds: number | null | undefined): string {
  if (seconds == null || Number.isNaN(seconds)) return "—";
  const total = Math.max(0, Math.floor(seconds));
  if (total === 0) return "0m";
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  if (h > 0) return `${h}h ${String(m).padStart(2, "0")}m`;
  return `${m}m`;
}
export function fmtUptime(sec: number): string {
  const d = Math.floor(sec / 86400), h = Math.floor((sec % 86400) / 3600), m = Math.floor((sec % 3600) / 60);
  if (d > 0) return `${d}d ${h}h`;
  if (h > 0) return `${h}h ${m}m`;
  return `${m}m ${Math.floor(sec % 60)}s`;
}
export function fmtBytes(n: number | null): string {
  if (n == null) return "unknown";
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}
