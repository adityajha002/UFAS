import { ApiError } from '../api/errors';

/** Enrollment codes are 20 hex chars (the server's randHex(10)). */
const CODE_RE = /^[0-9a-f]{20}$/;

/**
 * Accepts what a QR scan or hand-typed entry might contain and returns the canonical
 * lower-case code, or null. Manual entry may include spaces or dashes ("ab12-cd34-...").
 * ASSUMPTION: the QR encodes the bare code, or text containing it as a standalone token.
 */
export function normalizeCode(raw: string): string | null {
  const compact = raw.replace(/[\s-]/g, '').toLowerCase();
  if (CODE_RE.test(compact)) return compact;

  const lower = raw.toLowerCase();
  const matches = lower.match(/[0-9a-f]{20}/g) ?? [];
  for (const token of matches) {
    const index = lower.indexOf(token);
    const before = lower[index - 1] ?? '';
    const after = lower[index + token.length] ?? '';
    if (!/[0-9a-f]/.test(before) && !/[0-9a-f]/.test(after)) {
      return token;
    }
  }
  return null;
}

function looksLikeFacultyID(raw: string): boolean {
  const compact = raw.replace(/[\s-]/g, '').toLowerCase();
  return /^[0-9a-f]{32}$/i.test(compact);
}

export type EnrollFailure = 'invalid_format' | 'key_failed' | 'save_failed';
export type EnrollDetail = 'faculty_id' | 'invalid';
export class EnrollError extends Error {
  constructor(public code: EnrollFailure, public detail: EnrollDetail = 'invalid') { super(code); this.name = 'EnrollError'; }
}

export class NativeKeyUnavailableError extends Error {
  constructor() {
    super('AttendanceSecurity native module unavailable. Use a native development build (Android/iOS) or an EAS dev build.');
    this.name = 'NativeKeyUnavailableError';
  }
}

export type EnrollDeps = {
  generateKey(): Promise<{ publicKey: string }>;
  deleteKey(): Promise<void>;
  register(code: string, publicKey: string): Promise<{ faculty_id: string; name?: string }>;
  saveEnrollment(facultyId: string, name?: string): Promise<void>;
  clearAll(): Promise<void>;
};

/**
 * code -> new key -> register -> persist. If anything fails after the key exists, the key
 * (and any stored identity) is removed so the app never sits half-enrolled.
 */
export async function enroll(raw: string, d: EnrollDeps): Promise<{ facultyId: string; name?: string }> {
  if (looksLikeFacultyID(raw)) throw new EnrollError('invalid_format', 'faculty_id');
  const code = normalizeCode(raw);
  if (!code) throw new EnrollError('invalid_format');

  let publicKey: string;
  try {
    publicKey = (await d.generateKey()).publicKey;
  } catch (e) {
    console.warn('generateKey failed:', e);
    if (e instanceof Error && /AttendanceSecurity|native development build|Expo Go|web preview/i.test(e.message)) {
      throw new NativeKeyUnavailableError();
    }
    throw new EnrollError('key_failed');
  }

  try {
    const res = await d.register(code, publicKey);
    try {
      await d.saveEnrollment(res.faculty_id, res.name);
    } catch {
      throw new EnrollError('save_failed');
    }
    return { facultyId: res.faculty_id, name: res.name };
  } catch (e) {
    await d.deleteKey().catch(() => {});
    await d.clearAll().catch(() => {});
    throw e;
  }
}

export function enrollMessage(e: unknown): string {
  if (e instanceof ApiError) return e.message;
  if (e instanceof NativeKeyUnavailableError) {
    return 'This app needs a native development build to enrol the device. Run it on Android/iOS in a dev build or an EAS dev build, not Expo Go or the web preview.';
  }
  if (e instanceof EnrollError) {
    if (e.detail === 'faculty_id') {
      return 'This looks like a faculty ID (32 hex chars), not the 20-character enrollment code from the office. Use the office-issued code.';
    }
    switch (e.code) {
      case 'invalid_format': return 'That code does not look right. This is often a faculty ID pasted by mistake; use the 20-character enrollment code from the office (0–9, a–f).';
      case 'key_failed': return 'This device could not create a secure key. Make sure a screen lock is set, then try again.';
      case 'save_failed': return 'Could not save your enrolment on this device. Try again.';
    }
  }
  if (e instanceof Error && /AttendanceSecurity|native development build|Expo Go|web preview/i.test(e.message)) {
    return 'This app needs a native development build to enrol the device. Run it on Android/iOS in a dev build or an EAS dev build, not Expo Go or the web preview.';
  }
  return 'Something went wrong. Try again.';
}
