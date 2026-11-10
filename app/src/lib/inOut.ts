// Pure IN/OUT (v2) screen logic, kept free of React so it can be unit-tested.
import type { AttendanceV2Response, HistoryResponse, WindowStatus } from '../api/client';
import type { HistoryStatus } from './historyView';
import type { MarkState } from './markState';

type InOutAction = 'in' | 'out' | 'request_out';

/**
 * Which marking flow the screen uses, decided only by /history fetched in this session
 * (the server is the source of truth; a cached copy never decides):
 *  - 'v2'      the server reports `is_in` (SRV-2 or later): MARK IN / MARK OUT
 *  - 'v1'      an older server without `is_in`: the original single daily mark, unchanged
 *  - 'unknown' no server answer yet this session
 */
export type MarkMode = 'v1' | 'v2' | 'unknown';

export function markMode(data: HistoryResponse | null, fresh: boolean): MarkMode {
  if (!data || !fresh) return 'unknown';
  return typeof data.is_in === 'boolean' ? 'v2' : 'v1';
}

/** What the server last said: which action the button offers, and whether the window is open. */
export type InOutBase = { state: 'idle' | 'closed' | 'unknown'; action: InOutAction | null; pending?: boolean };

function isPendingRequest(data: HistoryResponse | null): boolean {
  return data?.pending_exit_request?.status === 'pending';
}

function windowOpen(window: WindowStatus | null | undefined): boolean {
  return !!window && window.open;
}

export function inOutBase(data: HistoryResponse | null, fresh: boolean): InOutBase {
  if (!data || markMode(data, fresh) !== 'v2') return { state: 'unknown', action: null };
  if (isPendingRequest(data)) return { state: 'closed', action: 'request_out', pending: true };
  if (!data.is_in) {
    const inOpen = data.in_window ? windowOpen(data.in_window) : data.window_open;
    return { state: inOpen ? 'idle' : 'closed', action: 'in' };
  }
  if (data.out_window) {
    if (windowOpen(data.out_window)) return { state: 'idle', action: 'out' };
    return { state: 'closed', action: 'request_out' };
  }
  return { state: data.window_open ? 'idle' : 'closed', action: 'out' };
}

/** Button look. Unknown state: spinner while the first /history is loading, otherwise disabled. */
export function inOutButton(
  base: InOutBase, busy: boolean, historyStatus: HistoryStatus,
): { state: MarkState; label?: string } {
  if (busy) return { state: 'working' };
  if (base.pending) return { state: 'marked', label: 'EXIT REQUEST PENDING' };
  if (base.state === 'unknown' || !base.action) {
    return { state: historyStatus === 'loading' ? 'working' : 'unknown' };
  }
  if (base.action === 'request_out') return { state: base.state === 'closed' ? 'closed' : 'idle', label: 'REQUEST OUT' };
  return { state: base.state, label: base.action === 'in' ? 'MARK IN' : 'MARK OUT' };
}

/** Line shown on the card: progress, then the last result, then what the base state means. */
export function inOutMessage(
  base: InOutBase, historyStatus: HistoryStatus, stage: string | null, notice: string | null,
): string | undefined {
  if (stage) return stage;
  if (notice) return notice;
  if (base.pending) return 'Exit request is pending — waiting for admin approval.';
  if (base.action === 'request_out') return 'OUT is closed. You can request an early exit for admin approval.';
  if (base.state === 'closed') return 'Attendance is not open right now';
  if (base.state === 'unknown') {
    if (historyStatus === 'loading') return 'Checking whether you are IN or OUT…';
    if (historyStatus === 'offline') return 'You are offline. Connect to the internet to mark IN or OUT.';
    if (historyStatus === 'error') return 'Could not check whether you are IN or OUT. Pull down the history to refresh.';
  }
  return undefined;
}

/**
 * "09:12" from an IST time such as "2026-11-10T09:12:00+05:30", "2026-11-10 09:12:00" or "09:12".
 * Reads the wall-clock digits as sent (the value is already IST). null if there is no HH:MM.
 */
export function clockHM(ist: string | null | undefined): string | null {
  if (!ist) return null;
  const m = /(?:^|[T\s])(\d{2}):(\d{2})/.exec(ist);
  return m ? `${m[1]}:${m[2]}` : null;
}

/** 12000 -> "3h 20m", 300 -> "0h 05m". */
export function formatWorked(seconds: number): string {
  const minutes = Math.max(0, Math.floor(seconds / 60));
  return `${Math.floor(minutes / 60)}h ${String(minutes % 60).padStart(2, '0')}m`;
}

/** "Marked IN at 09:12" / "Marked OUT — worked 3h 20m today". */
export function successMessage(res: AttendanceV2Response): string {
  if (res.action === 'in') {
    const t = clockHM(res.at_ist);
    return t ? `Marked IN at ${t}` : 'Marked IN';
  }
  return typeof res.today_worked_s === 'number' && Number.isFinite(res.today_worked_s)
    ? `Marked OUT — worked ${formatWorked(res.today_worked_s)} today`
    : 'Marked OUT';
}

/**
 * Today's state for the greeting card (IN/OUT flow only, from /history fetched this session):
 *   "Today: IN since 09:12 · worked 3h 20m"  "Today: OUT · worked 6h 20m"  "Today: not marked yet"
 * null when the state is not known (old server, or no fresh /history).
 */
export function todayLine(data: HistoryResponse | null, fresh: boolean): string | null {
  if (!data || markMode(data, fresh) !== 'v2') return null;
  const worked = typeof data.today_worked_s === 'number' && data.today_worked_s > 0 ? formatWorked(data.today_worked_s) : null;
  if (data.pending_exit_request?.status === 'pending') {
    const t = clockHM(data.pending_exit_request.requested_at_ist);
    return t ? `Exit request pending since ${t}` : 'Exit request pending';
  }
  if (data.is_in) {
    const t = clockHM(data.open_since_ist);
    const parts = [t ? `Today: IN since ${t}` : 'Today: IN'];
    if (worked) parts.push(`worked ${worked}`);
    return parts.join(' · ');
  }
  return worked ? `Today: OUT · worked ${worked}` : 'Today: not marked yet';
}

export function windowSummary(data: HistoryResponse | null, fresh: boolean): string[] {
  if (!data || markMode(data, fresh) !== 'v2') return [];
  const inLabel = data.in_window?.open ? 'ENTRY: Open now' : 'ENTRY: Closed';
  const outLabel = data.out_window?.open ? 'EXIT: Open now' : 'EXIT: Closed';
  const inSpec = data.in_window?.active ? ` · ${data.in_window.active.start}–${data.in_window.active.end}` :
    data.in_window?.next ? ` · opens ${data.in_window.next.start}` : '';
  const outSpec = data.out_window?.active ? ` · ${data.out_window.active.start}–${data.out_window.active.end}` :
    data.out_window?.next ? ` · opens ${data.out_window.next.start}` : '';
  return [`${inLabel}${inSpec}`, `${outLabel}${outSpec}`];
}
