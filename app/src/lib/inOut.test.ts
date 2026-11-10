import {
  clockHM, formatWorked, inOutBase, inOutButton, inOutMessage, markMode, successMessage, todayLine,
} from './inOut';
import type { AttendanceV2Response, HistoryResponse } from '../api/client';

const V1: HistoryResponse = { name: 'Dr X', marked_today: false, window_open: true, days: [] };
const OUT_NOW: HistoryResponse = { ...V1, is_in: false, open_since_ist: null, today_worked_s: 0 };
const IN_NOW: HistoryResponse = { ...V1, is_in: true, open_since_ist: '2026-11-10T09:12:00+05:30', today_worked_s: 0 };

describe('markMode', () => {
  it('unknown without data or without a fresh fetch', () => {
    expect(markMode(null, true)).toBe('unknown');
    expect(markMode(IN_NOW, false)).toBe('unknown'); // a cached is_in never decides
  });
  it('prefers the IN/OUT flow for fresh history data', () => {
    expect(markMode(IN_NOW, true)).toBe('v2');
    expect(markMode(OUT_NOW, true)).toBe('v2');
    expect(markMode(V1, true)).toBe('v2');
  });
});

describe('inOutBase', () => {
  it('OUT now -> offer IN; IN now -> offer OUT', () => {
    expect(inOutBase(OUT_NOW, true)).toEqual({ state: 'idle', action: 'in' });
    expect(inOutBase(IN_NOW, true)).toEqual({ state: 'idle', action: 'out' });
  });
  it('closed window keeps the action', () => {
    expect(inOutBase({ ...IN_NOW, window_open: false }, true)).toEqual({ state: 'closed', action: 'out' });
  });
  it('uses the new IN/OUT flow whenever the history is fresh', () => {
    expect(inOutBase(IN_NOW, false)).toEqual({ state: 'unknown', action: null });
    expect(inOutBase(V1, true)).toEqual({ state: 'idle', action: 'in' });
    expect(inOutBase(null, false)).toEqual({ state: 'unknown', action: null });
  });
});

describe('inOutButton', () => {
  const unknown = { state: 'unknown', action: null } as const;
  it('labels by action', () => {
    expect(inOutButton({ state: 'idle', action: 'in' }, false, 'ready')).toEqual({ state: 'idle', label: 'MARK IN' });
    expect(inOutButton({ state: 'idle', action: 'out' }, false, 'ready')).toEqual({ state: 'idle', label: 'MARK OUT' });
    expect(inOutButton({ state: 'closed', action: 'out' }, false, 'offline')).toEqual({ state: 'closed', label: 'MARK OUT' });
  });
  it('spinner while a request runs, whatever the state', () => {
    expect(inOutButton({ state: 'idle', action: 'in' }, true, 'ready')).toEqual({ state: 'working' });
  });
  it('unknown: spinner while loading, disabled otherwise', () => {
    expect(inOutButton(unknown, false, 'loading')).toEqual({ state: 'working' });
    expect(inOutButton(unknown, false, 'offline')).toEqual({ state: 'unknown' });
    expect(inOutButton(unknown, false, 'error')).toEqual({ state: 'unknown' });
  });
});

describe('inOutMessage', () => {
  const idle = { state: 'idle', action: 'in' } as const;
  const unknown = { state: 'unknown', action: null } as const;
  it('stage beats notice beats base', () => {
    expect(inOutMessage(idle, 'ready', 'Verifying…', 'Marked IN at 09:12')).toBe('Verifying…');
    expect(inOutMessage({ state: 'closed', action: 'out' }, 'ready', null, "You're not marked IN.")).toBe("You're not marked IN.");
    expect(inOutMessage({ state: 'closed', action: 'out' }, 'ready', null, null)).toBe('Attendance is not open right now');
  });
  it('nothing extra when idle and known', () => {
    expect(inOutMessage(idle, 'ready', null, null)).toBeUndefined();
  });
  it('explains an unknown state', () => {
    expect(inOutMessage(unknown, 'loading', null, null)).toMatch(/Checking/);
    expect(inOutMessage(unknown, 'offline', null, null)).toMatch(/offline/);
    expect(inOutMessage(unknown, 'error', null, null)).toMatch(/Could not check/);
  });
});

describe('clockHM', () => {
  it.each([
    ['2026-11-10T09:12:00+05:30', '09:12'],
    ['2026-11-10T17:05:59+05:30', '17:05'],
    ['2026-11-10 09:12:00', '09:12'],
    ['09:12', '09:12'],
  ])('%s -> %s', (input, out) => { expect(clockHM(input)).toBe(out); });
  it.each([null, undefined, '', '2026-11-10', 'soon'])('%j -> null', (input) => {
    expect(clockHM(input)).toBeNull();
  });
});

describe('formatWorked', () => {
  it.each([[12000, '3h 20m'], [300, '0h 05m'], [21900, '6h 05m'], [59, '0h 00m'], [0, '0h 00m'], [-5, '0h 00m']])(
    '%d s -> %s', (sec, out) => { expect(formatWorked(sec)).toBe(out); },
  );
});

describe('successMessage', () => {
  const res: AttendanceV2Response = {
    action: 'in', day: '2026-11-10', at_ist: '2026-11-10T09:12:00+05:30', distance_m: 40, is_in: true, today_worked_s: 0,
  };
  it('IN shows the time', () => { expect(successMessage(res)).toBe('Marked IN at 09:12'); });
  it('IN without a readable time', () => { expect(successMessage({ ...res, at_ist: 'x' })).toBe('Marked IN'); });
  it('OUT shows hours worked today', () => {
    expect(successMessage({ ...res, action: 'out', is_in: false, today_worked_s: 12000 })).toBe('Marked OUT — worked 3h 20m today');
  });
  it('OUT without hours', () => {
    expect(successMessage({ ...res, action: 'out', is_in: false, today_worked_s: undefined as unknown as number })).toBe('Marked OUT');
  });
});

describe('todayLine', () => {
  it('IN: since when, plus hours already worked today', () => {
    expect(todayLine(IN_NOW, true)).toBe('Today: IN since 09:12');
    expect(todayLine({ ...IN_NOW, today_worked_s: 12000 }, true)).toBe('Today: IN since 09:12 · worked 3h 20m');
    expect(todayLine({ ...IN_NOW, open_since_ist: null }, true)).toBe('Today: IN');
  });
  it('OUT: hours worked, or not marked yet', () => {
    expect(todayLine({ ...OUT_NOW, today_worked_s: 22800 }, true)).toBe('Today: OUT · worked 6h 20m');
    expect(todayLine(OUT_NOW, true)).toBe('Today: not marked yet');
  });
  it('still shows a reasonable line for the forced IN/OUT flow', () => {
    expect(todayLine(IN_NOW, false)).toBeNull(); // cache only
    expect(todayLine(V1, true)).toBe('Today: not marked yet');
    expect(todayLine(null, true)).toBeNull();
  });
});
