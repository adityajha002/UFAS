// India has no daylight saving, so a fixed UTC+5:30 offset is enough.
// A "campus date" is a Date shifted by the offset and read ONLY with getUTC* methods.
const IST_OFFSET_MS = 5.5 * 60 * 60 * 1000;
export const toCampusDate = (ms: number): Date => new Date(ms + IST_OFFSET_MS);
const pad = (n: number) => String(n).padStart(2, '0');
export const ymd = (d: Date): string => `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())}`;

export const addDays = (d: Date, n: number): Date => {
  const x = new Date(d.getTime());
  x.setUTCDate(x.getUTCDate() + n);
  return x;
};

/** Midnight (UTC fields) of the Monday on or before d. */
export function mondayOf(d: Date): Date {
  const x = new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), d.getUTCDate()));
  return addDays(x, -((x.getUTCDay() + 6) % 7));
}

export function subtractMonths(d: Date, months: number): Date {
  const x = new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth() - months, 1));
  const last = new Date(Date.UTC(x.getUTCFullYear(), x.getUTCMonth() + 1, 0)).getUTCDate();
  x.setUTCDate(Math.min(d.getUTCDate(), last));
  return x;
}
