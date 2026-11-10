import AsyncStorage from '@react-native-async-storage/async-storage';
import type { HistoryResponse } from '../api/client';

const K = { facultyId: 'faculty_id', name: 'faculty_name' } as const;
const HISTORY_PREFIX = 'history_cache:';

/**
 * Stored history format version.
 *  - (none) written by the app before APP-3: `{ data, savedAt }` with only day strings
 *  - 2      `{ v: 2, data, savedAt }`, `data` may carry the IN/OUT and per-day status fields
 */
export const HISTORY_CACHE_VERSION = 2;

export type CachedHistory = { data: HistoryResponse; savedAt: number };

export function historyCacheKey(facultyId: string): string {
  return `${HISTORY_PREFIX}${facultyId}`;
}

/**
 * Turns a stored value into a usable cache entry, or null if it is unreadable or from a newer
 * app version. Entries from before versioning keep only the original fields, so their days
 * show as complete (green) and never as incomplete (red) until the next /history replaces them.
 */
export function migrateCachedHistory(raw: unknown): CachedHistory | null {
  const o = raw as { v?: unknown; data?: Partial<HistoryResponse>; savedAt?: unknown } | null;
  if (!o || typeof o !== 'object' || typeof o.savedAt !== 'number' || !o.data || typeof o.data !== 'object') return null;
  if (!Array.isArray(o.data.days) || !o.data.days.every((d) => typeof d === 'string')) return null;

  if (o.v === undefined) {
    const d = o.data;
    return {
      savedAt: o.savedAt,
      data: {
        name: typeof d.name === 'string' ? d.name : '',
        marked_today: d.marked_today === true,
        window_open: d.window_open === true,
        days: d.days as string[],
      },
    };
  }
  if (o.v === HISTORY_CACHE_VERSION) return { data: o.data as HistoryResponse, savedAt: o.savedAt };
  return null; // unknown (newer) format: ignore it, the next /history will replace it
}

export async function saveEnrollment(facultyId: string, name?: string): Promise<void> {
  await AsyncStorage.setItem(K.facultyId, facultyId);
  if (name) await AsyncStorage.setItem(K.name, name);
}

export async function loadEnrollment(): Promise<{ facultyId: string; name: string | null } | null> {
  const [facultyId, name] = await Promise.all([AsyncStorage.getItem(K.facultyId), AsyncStorage.getItem(K.name)]);
  return facultyId ? { facultyId, name } : null;
}

export async function saveHistory(facultyId: string, data: HistoryResponse): Promise<void> {
  const value = { v: HISTORY_CACHE_VERSION, data, savedAt: Date.now() };
  await AsyncStorage.setItem(historyCacheKey(facultyId), JSON.stringify(value));
}

export async function loadHistory(facultyId: string): Promise<CachedHistory | null> {
  const raw = await AsyncStorage.getItem(historyCacheKey(facultyId));
  if (!raw) return null;
  try { return migrateCachedHistory(JSON.parse(raw)); } catch { return null; }
}

/** Wipes everything tied to the enrolled identity (revoked, or re-enrolling). */
export async function clearAll(): Promise<void> {
  await AsyncStorage.clear();
}
