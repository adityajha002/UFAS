import { DAY_STYLE, dayDetailLookup, dayDetailText, hasDayStatus, parseDayStatus, workedText } from './dayStatus';
import { colors } from '../theme/colors';
import type { HistoryResponse } from '../api/client';

const NOW = Date.parse('2026-11-10T06:00:00Z'); // Tue 10 Nov 2026, 11:30 IST
const BASE: HistoryResponse = { name: 'Dr X', marked_today: true, window_open: true, days: [], today: '2026-11-10' };

describe('status -> colour', () => {
  it('green complete, amber in progress, red incomplete, grey no record', () => {
    expect(DAY_STYLE.COMPLETE.bg).toBe(colors.green);
    expect(DAY_STYLE.IN_PROGRESS.bg).toBe(colors.amber);
    expect(DAY_STYLE.INCOMPLETE.bg).toBe(colors.red);
    expect(DAY_STYLE.NONE.bg).toBe(colors.chipGrey);
  });
  it('amber and red are distinct from the other theme colours', () => {
    const others = [colors.green, colors.chipGrey, colors.orange, colors.yellow];
    expect(others).not.toContain(colors.amber);
    expect(others).not.toContain(colors.red);
  });
});

describe('parseDayStatus', () => {
  const want = new Map([
    ['2026-11-09', { status: 'COMPLETE', workedS: 21900 }],
    ['2026-11-10', { status: 'IN_PROGRESS', workedS: null }],
  ]);
  it('accepts an array of { day, status, worked_s }', () => {
    expect(parseDayStatus([
      { day: '2026-11-09', status: 'COMPLETE', worked_s: 21900 },
      { day: '2026-11-10', status: 'IN_PROGRESS', worked_s: null },
    ])).toEqual(want);
  });
  it('accepts a map of day -> { status, worked_s }', () => {
    expect(parseDayStatus({ '2026-11-09': { status: 'COMPLETE', worked_s: 21900 }, '2026-11-10': { status: 'IN_PROGRESS' } })).toEqual(want);
  });
  it('accepts a map of day -> status', () => {
    expect(parseDayStatus({ '2026-11-09': 'COMPLETE' }).get('2026-11-09')).toEqual({ status: 'COMPLETE', workedS: null });
  });
  it.each([undefined, null, 'x', 42, [null, 1, { day: 'bad', status: 'COMPLETE' }], { '2026-11-09': 'DONE' }, { yesterday: 'COMPLETE' }])(
    'skips what it does not understand: %j', (raw) => { expect(parseDayStatus(raw).size).toBe(0); },
  );
  it('ignores a negative or non-numeric worked_s', () => {
    expect(parseDayStatus([{ day: '2026-11-09', status: 'COMPLETE', worked_s: -1 }]).get('2026-11-09')!.workedS).toBeNull();
    expect(parseDayStatus([{ day: '2026-11-09', status: 'COMPLETE', worked_s: '6h' }]).get('2026-11-09')!.workedS).toBeNull();
  });
});

describe('dayDetailLookup', () => {
  it('server status wins; days without one are complete (legacy); others are grey', () => {
    const at = dayDetailLookup({
      ...BASE,
      days: ['2026-11-03', '2026-11-04', '2026-11-05'],
      day_status: [
        { day: '2026-11-04', status: 'INCOMPLETE', worked_s: null },
        { day: '2026-11-05', status: 'COMPLETE', worked_s: 3600 },
      ],
    }, NOW);
    expect(at('2026-11-03')).toEqual({ status: 'COMPLETE', workedS: null });
    expect(at('2026-11-04')).toEqual({ status: 'INCOMPLETE', workedS: null });
    expect(at('2026-11-05')).toEqual({ status: 'COMPLETE', workedS: 3600 });
    expect(at('2026-11-06')).toEqual({ status: 'NONE', workedS: null });
  });

  it('IN_PROGRESS is amber only on the server\'s today; an earlier one shows as incomplete', () => {
    const at = dayDetailLookup({
      ...BASE,
      day_status: [
        { day: '2026-11-09', status: 'IN_PROGRESS', worked_s: 0 },
        { day: '2026-11-10', status: 'IN_PROGRESS', worked_s: 1800 },
      ],
    }, NOW);
    expect(at('2026-11-09').status).toBe('INCOMPLETE');
    expect(at('2026-11-10')).toEqual({ status: 'IN_PROGRESS', workedS: 1800 });
  });

  it('uses the server\'s today, not the phone\'s, when it has one', () => {
    const at = dayDetailLookup({ ...BASE, today: '2026-11-09', day_status: [{ day: '2026-11-09', status: 'IN_PROGRESS' }] }, NOW);
    expect(at('2026-11-09').status).toBe('IN_PROGRESS');
  });

  it('today in progress without hours falls back to today_worked_s', () => {
    const at = dayDetailLookup({ ...BASE, today_worked_s: 5400, day_status: [{ day: '2026-11-10', status: 'IN_PROGRESS' }] }, NOW);
    expect(at('2026-11-10')).toEqual({ status: 'IN_PROGRESS', workedS: 5400 });
  });

  it('old server (days only): every marked day is green, never red', () => {
    const at = dayDetailLookup({ ...BASE, days: ['2026-11-09'] }, NOW);
    expect(at('2026-11-09')).toEqual({ status: 'COMPLETE', workedS: null });
  });

  it('no data: all grey', () => {
    expect(dayDetailLookup(null, NOW)('2026-11-10').status).toBe('NONE');
  });
});

describe('hasDayStatus', () => {
  it('only when the server sent usable statuses', () => {
    expect(hasDayStatus(null)).toBe(false);
    expect(hasDayStatus({ ...BASE, days: ['2026-11-09'] })).toBe(false);
    expect(hasDayStatus({ ...BASE, day_status: [] })).toBe(false);
    expect(hasDayStatus({ ...BASE, day_status: [{ day: '2026-11-09', status: 'COMPLETE' }] })).toBe(true);
  });
});

describe('hours text', () => {
  it('shows hours when known', () => {
    expect(workedText({ status: 'COMPLETE', workedS: 21900 })).toBe('6h 05m');
    expect(workedText({ status: 'INCOMPLETE', workedS: 3600 })).toBe('1h 00m');
    expect(workedText({ status: 'IN_PROGRESS', workedS: 1800 })).toBe('0h 30m');
  });
  it('no hours for legacy days, grey days, or nothing worked yet today', () => {
    expect(workedText({ status: 'COMPLETE', workedS: null })).toBeNull();
    expect(workedText({ status: 'NONE', workedS: 100 })).toBeNull();
    expect(workedText({ status: 'IN_PROGRESS', workedS: 0 })).toBeNull();
  });
  it('detail line for a tapped chip', () => {
    expect(dayDetailText('TUES', '2026-11-10', { status: 'COMPLETE', workedS: 21900 })).toBe('TUESDAY 10/11 · COMPLETE · 6h 05m');
    expect(dayDetailText('MON', '2026-11-02', { status: 'COMPLETE', workedS: null })).toBe('MONDAY 2/11 · COMPLETE');
    expect(dayDetailText('FRI', '2026-11-06', { status: 'NONE', workedS: null })).toBe('FRIDAY 6/11 · NO RECORD');
  });
});
