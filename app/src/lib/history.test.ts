jest.mock('@react-native-async-storage/async-storage', () => {
  const store = new Map<string, string>();
  return {
    __esModule: true,
    default: {
      getItem: jest.fn(async (key: string) => store.get(key) ?? null),
      setItem: jest.fn(async (key: string, value: string) => { store.set(key, value); }),
      removeItem: jest.fn(async (key: string) => { store.delete(key); }),
      multiRemove: jest.fn(async (keys: string[]) => { keys.forEach((k) => store.delete(k)); }),
      getAllKeys: jest.fn(async () => Array.from(store.keys())),
      clear: jest.fn(async () => { store.clear(); }),
    },
  };
});

import { fetchHistory } from './fetchHistory';
import { buttonBase, classifyHistoryError, updatedLabel } from './historyView';
import AsyncStorage from '@react-native-async-storage/async-storage';
import { clearAll, historyCacheKey, HISTORY_CACHE_VERSION, loadHistory, migrateCachedHistory, saveHistory } from './storage';
import { dayDetailLookup } from './dayStatus';
import { ApiError } from '../api/errors';

const DATA = { name: 'Dr X', marked_today: false, window_open: true, days: ['2026-10-26'] };
// 2026-10-26 10:00 IST
const T = Date.parse('2026-10-26T04:30:00Z');

describe('fetchHistory', () => {
  it('challenge, sign, history in order', async () => {
    const log: string[] = [];
    const out = await fetchHistory('f1', {
      challenge: async () => { log.push('challenge'); return { challenge_id: 'c1', nonce: 'n1' }; },
      signHistory: async (f) => { log.push(`sign:${f.challengeId}:${f.nonce}:${f.facultyId}`); return { signature: 'S' }; },
      history: async (a) => { log.push(`history:${a.signature}`); return DATA; },
    });
    expect(out).toEqual(DATA);
    expect(log).toEqual(['challenge', 'sign:c1:n1:f1', 'history:S']);
  });
});

describe('classifyHistoryError', () => {
  it('maps errors to statuses', () => {
    expect(classifyHistoryError(new ApiError('not_enrolled', 403))).toBe('revoked');
    expect(classifyHistoryError(new ApiError('network'))).toBe('offline');
    expect(classifyHistoryError(new ApiError('timeout'))).toBe('offline');
    expect(classifyHistoryError(new ApiError('server', 500))).toBe('error');
    expect(classifyHistoryError(new Error('Call to function failed: key_missing'))).toBe('key_missing');
    expect(classifyHistoryError(new Error('boom'))).toBe('error');
  });
});

describe('buttonBase', () => {
  it('idle with no data', () => { expect(buttonBase(null, null, T, false)).toBe('idle'); });
  it('marked from fresh data', () => {
    expect(buttonBase({ ...DATA, marked_today: true }, T, T, true)).toBe('marked');
  });
  it('marked from a cache saved today', () => {
    expect(buttonBase({ ...DATA, marked_today: true }, T - 3600_000, T, false)).toBe('marked');
  });
  it('ignores marked_today from yesterday\'s cache', () => {
    expect(buttonBase({ ...DATA, marked_today: true }, T - 24 * 3600_000, T, false)).toBe('idle');
  });
  it('closed only when fresh', () => {
    expect(buttonBase({ ...DATA, window_open: false }, T, T, true)).toBe('closed');
    expect(buttonBase({ ...DATA, window_open: false }, T, T, false)).toBe('idle');
  });
  it('open window is idle', () => { expect(buttonBase(DATA, T, T, true)).toBe('idle'); });
});

describe('history cache', () => {
  beforeEach(async () => { await clearAll(); });
  afterEach(async () => { await clearAll(); });

  it('keeps each faculty’s cached history separate', async () => {
    const one = { name: 'Dr A', marked_today: true, window_open: true, days: ['2026-10-26'] };
    const two = { name: 'Dr B', marked_today: false, window_open: true, days: ['2026-10-27'] };

    await saveHistory('f1', one);
    await saveHistory('f2', two);

    expect(await loadHistory('f1')).toMatchObject({ data: one });
    expect(await loadHistory('f2')).toMatchObject({ data: two });
    expect(await loadHistory('f3')).toBeNull();
  });

  it('writes version 2 and reads it back unchanged, including the new fields', async () => {
    const v2 = { ...DATA, today: '2026-10-26', is_in: true, today_worked_s: 0, day_status: [{ day: '2026-10-23', status: 'INCOMPLETE' }] };
    await saveHistory('f1', v2);
    const stored = JSON.parse((await AsyncStorage.getItem(historyCacheKey('f1')))!);
    expect(stored.v).toBe(HISTORY_CACHE_VERSION);
    expect(await loadHistory('f1')).toMatchObject({ data: v2 });
  });

  it('reads a cache written by the old app (no version) as days only, so nothing turns red', async () => {
    const old = { data: { ...DATA, days: ['2026-10-23'], day_status: [{ day: '2026-10-23', status: 'INCOMPLETE' }] }, savedAt: 123 };
    await AsyncStorage.setItem(historyCacheKey('f1'), JSON.stringify(old));
    const got = await loadHistory('f1');
    expect(got).toEqual({ savedAt: 123, data: { name: 'Dr X', marked_today: false, window_open: true, days: ['2026-10-23'] } });
    expect(dayDetailLookup(got!.data, T)('2026-10-23').status).toBe('COMPLETE');
  });

  it.each([
    ['garbage', 'not json'],
    ['a newer format', JSON.stringify({ v: 3, data: DATA, savedAt: 1 })],
    ['no savedAt', JSON.stringify({ data: DATA })],
    ['days not a list', JSON.stringify({ data: { ...DATA, days: 'x' }, savedAt: 1 })],
  ])('ignores %s without crashing', async (_name, raw) => {
    await AsyncStorage.setItem(historyCacheKey('f1'), raw);
    expect(await loadHistory('f1')).toBeNull();
  });
});

describe('migrateCachedHistory', () => {
  it('null for non-objects', () => {
    expect(migrateCachedHistory(null)).toBeNull();
    expect(migrateCachedHistory(42)).toBeNull();
  });
});

describe('updatedLabel', () => {
  it('uses campus time', () => {
    expect(updatedLabel(T, T)).toBe('today 10:00');
    expect(updatedLabel(T - 24 * 3600_000, T)).toBe('25/10 10:00');
  });
});
