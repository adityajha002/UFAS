import { greeting } from './greeting';
import { groupByWeek, monthSummary, monthSummaryText } from './groupByWeek';
import { weekCaptionText, weekRange } from './weekLabel';
import { mondayOf, toCampusDate } from './dates';
import { dayDetailLookup } from './dayStatus';

const fromDays = (days: string[], now: number) =>
  dayDetailLookup({ name: '', marked_today: false, window_open: true, days }, now);

test('greeting boundaries', () => {
  expect(greeting(new Date(2026, 8, 1, 4, 59))).toBe('GOOD EVENING');
  expect(greeting(new Date(2026, 8, 1, 5, 0))).toBe('GOOD MORNING');
  expect(greeting(new Date(2026, 8, 1, 12, 0))).toBe('GOOD AFTERNOON');
  expect(greeting(new Date(2026, 8, 1, 17, 0))).toBe('GOOD EVENING');
});

test('week 31/08 to 6/09 is SEPT - 1ST WEEK', () => {
  const monday = mondayOf(toCampusDate(Date.parse('2026-09-02T00:00:00Z')));
  expect(weekRange(monday)).toBe('31/08/26 - 6/09/26');
  expect(weekCaptionText(monday)).toBe('SEPT - 1ST WEEK');
});

test('server campus dates mark exactly those days', () => {
  const now = Date.parse('2026-09-02T06:00:00Z');
  const weeks = groupByWeek(fromDays(['2026-09-02'], now), now);
  expect(weeks[0].days.find((d) => d.date === '2026-09-02')!.status).toBe('COMPLETE');
  expect(weeks[0].days.find((d) => d.date === '2026-09-01')!.status).toBe('NONE');
});

test('dates outside the window or on weekends are ignored', () => {
  const now = Date.parse('2026-09-02T06:00:00Z');
  const weeks = groupByWeek(fromDays(['2020-01-01', '2026-09-05'], now), now);
  expect(weeks.flatMap((w) => w.days).some((d) => d.status !== 'NONE')).toBe(false);
});

test('empty days gives all-grey weeks', () => {
  const now = Date.parse('2026-09-02T06:00:00Z');
  expect(groupByWeek(fromDays([], now), now)[0].days.every((d) => d.status === 'NONE')).toBe(true);
});

test('weeks are newest first and include the current week', () => {
  const now = Date.parse('2026-10-24T06:00:00Z');
  const weeks = groupByWeek(fromDays([], now), now);
  expect(weeks[0].monday.getUTCDate()).toBe(19);
  expect(weeks.length).toBeGreaterThanOrEqual(9);
  expect(weeks.every((w) => w.days.length === 5)).toBe(true);
});

test('each day carries its status and hours', () => {
  const now = Date.parse('2026-11-10T06:00:00Z'); // Tuesday
  const data = {
    name: '', marked_today: true, window_open: true, today: '2026-11-10',
    days: ['2026-11-09', '2026-11-10'],
    day_status: [
      { day: '2026-11-09', status: 'COMPLETE', worked_s: 21900 },
      { day: '2026-11-10', status: 'IN_PROGRESS', worked_s: 0 },
    ],
  };
  const [week] = groupByWeek(dayDetailLookup(data, now), now);
  expect(week.days.map((d) => [d.label, d.status, d.workedS])).toEqual([
    ['MON', 'COMPLETE', 21900], ['TUES', 'IN_PROGRESS', 0], ['WEDS', 'NONE', null], ['THU', 'NONE', null], ['FRI', 'NONE', null],
  ]);
  expect(week.days.filter((d) => d.isToday).map((d) => d.date)).toEqual(['2026-11-10']);
});

test('month summary counts only this campus month', () => {
  const now = Date.parse('2026-11-10T06:00:00Z');
  const data = {
    name: '', marked_today: true, window_open: true, today: '2026-11-10',
    days: ['2026-10-30', '2026-11-02', '2026-11-03', '2026-11-04', '2026-11-09', '2026-11-10'],
    day_status: [
      { day: '2026-11-04', status: 'INCOMPLETE' },
      { day: '2026-11-10', status: 'IN_PROGRESS' },
    ],
  };
  const m = monthSummary(groupByWeek(dayDetailLookup(data, now), now), now);
  expect(m).toEqual({ month: 'NOVEMBER', complete: 3, inProgress: 1, incomplete: 1 });
  expect(monthSummaryText(m)).toBe('NOVEMBER · 3 COMPLETE · 1 IN PROGRESS · 1 INCOMPLETE');
  expect(monthSummaryText({ month: 'MAY', complete: 0, inProgress: 0, incomplete: 0 })).toBe('MAY · 0 COMPLETE');
});
