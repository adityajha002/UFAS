import { ApiError } from '../api/errors';
import type { HistoryResponse } from '../api/client';
import type { MarkState } from './markState';
import { toCampusDate, ymd } from './dates';

export type HistoryStatus = 'loading' | 'ready' | 'offline' | 'error' | 'revoked' | 'key_missing';

export function classifyHistoryError(e: unknown): Exclude<HistoryStatus, 'loading' | 'ready'> {
  if (e instanceof ApiError) {
    if (e.code === 'not_enrolled') return 'revoked';
    if (e.code === 'network' || e.code === 'timeout') return 'offline';
    return 'error';
  }
  // Signer throws IllegalStateException("key_missing") when the Keystore key is gone.
  if (String((e as { message?: unknown })?.message ?? '').includes('key_missing')) return 'key_missing';
  return 'error';
}

export function historyErrorMessage(e: unknown): string {
  return e instanceof ApiError ? e.message : 'Could not load your attendance history.';
}

const sameCampusDay = (a: number, b: number) => ymd(toCampusDate(a)) === ymd(toCampusDate(b));

/**
 * Button state from the last known server data.
 *  - "marked" is trusted from a cache only if it was saved today (a marked day never un-marks).
 *  - "closed" is trusted only from data fetched this session; a stale window_open could be wrong.
 *  - otherwise "idle": the server decides when the person taps.
 */
export function buttonBase(
  data: HistoryResponse | null, savedAt: number | null, nowMs: number, fresh: boolean,
): MarkState {
  if (!data || savedAt === null) return 'idle';
  if (data.marked_today && sameCampusDay(savedAt, nowMs)) return 'marked';
  if (fresh && !data.marked_today && !data.window_open) return 'closed';
  return 'idle';
}

const pad = (n: number) => String(n).padStart(2, '0');
/** "today 14:05" or "24/10 14:05", in campus time. */
export function updatedLabel(savedAt: number, nowMs: number): string {
  const d = toCampusDate(savedAt);
  const hhmm = `${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}`;
  return sameCampusDay(savedAt, nowMs) ? `today ${hhmm}` : `${d.getUTCDate()}/${pad(d.getUTCMonth() + 1)} ${hhmm}`;
}
