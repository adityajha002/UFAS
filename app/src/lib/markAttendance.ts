import { ApiError } from '../api/errors';
import type { AttendAction, AttendanceV2Response } from '../api/client';
import type { Fix } from './fix';

export const CHALLENGE_TTL_MS = 60_000;
const SAFETY_MS = 5_000; // fetch a new challenge if the current one is this close to expiring

export type MarkStage = 'locating' | 'verifying';
export type MarkResult = { status: 'success' | 'already_marked' };

type SignFields = {
  challengeId: string; nonce: string; facultyId: string;
  latMicro: number; lonMicro: number; accuracyCm: number;
};
type SubmitArgs = SignFields & { signature: string };

type FlowDeps = {
  getFix(): Promise<Fix>;
  challenge(facultyId: string): Promise<{ challenge_id: string; nonce: string }>;
  now(): number;
};

export type MarkDeps = FlowDeps & {
  signAttendance(f: SignFields): Promise<{ signature: string }>;
  attendance(a: SubmitArgs): Promise<unknown>;
};

/**
 * fix -> challenge -> `attempt` (sign + submit), with the server's retry rules:
 *  - poor_accuracy / outside_area do not consume the challenge: take a new fix, reuse it (max 2 / 1 retries)
 *  - challenge_expired / challenge_used / challenge_invalid: fetch a new challenge and retry (max 2)
 * Every other ApiError (and any non-ApiError) is thrown.
 */
async function withRetries<T>(
  facultyId: string, d: FlowDeps, onStage: (s: MarkStage) => void,
  attempt: (fields: SignFields) => Promise<T>,
): Promise<T> {
  let ch: { id: string; nonce: string; at: number } | null = null;
  let accuracyRetries = 0, areaRetries = 0, renewals = 0;

  for (;;) {
    onStage('locating');
    const fix = await d.getFix();

    onStage('verifying');
    if (!ch || d.now() - ch.at > CHALLENGE_TTL_MS - SAFETY_MS) {
      const c = await d.challenge(facultyId);
      ch = { id: c.challenge_id, nonce: c.nonce, at: d.now() };
    }

    try {
      return await attempt({ challengeId: ch.id, nonce: ch.nonce, facultyId, ...fix });
    } catch (e) {
      if (!(e instanceof ApiError)) throw e;
      switch (e.code) {
        case 'poor_accuracy': if (accuracyRetries++ < 2) continue; throw e;
        case 'outside_area': if (areaRetries++ < 1) continue; throw e;
        case 'challenge_expired':
        case 'challenge_used':
        case 'challenge_invalid': if (renewals++ < 2) { ch = null; continue; } throw e;
        default: throw e;
      }
    }
  }
}

/**
 * v1 (single daily mark, `attend.v1`). Kept unchanged as the rollback path.
 * already_marked resolves as { status: 'already_marked' }; every other failure is thrown.
 */
export async function markAttendance(
  facultyId: string, d: MarkDeps, onStage: (s: MarkStage) => void = () => {},
): Promise<MarkResult> {
  try {
    await withRetries(facultyId, d, onStage, async (fields) => {
      const { signature } = await d.signAttendance(fields);
      await d.attendance({ ...fields, signature });
    });
    return { status: 'success' };
  } catch (e) {
    if (e instanceof ApiError && e.code === 'already_marked') return { status: 'already_marked' };
    throw e;
  }
}

// ---- v2 (IN/OUT, `attend.v2`) ----

export type MarkV2Deps = FlowDeps & {
  signAttendanceV2(f: SignFields & { action: AttendAction }): Promise<{ signature: string }>;
  attendanceV2(a: SubmitArgs & { action: AttendAction }): Promise<AttendanceV2Response>;
};

export type MarkV2Result =
  | { status: 'success'; response: AttendanceV2Response }
  /** The app's idea of IN/OUT was stale: show the message and refresh /history; do not retry. */
  | { status: 'already_in' | 'not_in' };

/**
 * Marks IN or OUT with the same fix/challenge/retry rules as v1. OUT is checked against the
 * geofence and window too, so it can fail with closed / outside_area / poor_accuracy.
 * already_in / not_in resolve (see MarkV2Result); every other failure is thrown.
 */
export async function markAttendanceV2(
  facultyId: string, action: AttendAction, d: MarkV2Deps, onStage: (s: MarkStage) => void = () => {},
): Promise<MarkV2Result> {
  try {
    const response = await withRetries(facultyId, d, onStage, async (fields) => {
      const { signature } = await d.signAttendanceV2({ ...fields, action });
      return d.attendanceV2({ ...fields, action, signature });
    });
    return { status: 'success', response };
  } catch (e) {
    if (e instanceof ApiError && (e.code === 'already_in' || e.code === 'not_in')) return { status: e.code };
    throw e;
  }
}
