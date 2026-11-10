import { ApiError, mapServerError } from './errors';
import type { AttendAction } from '../native/attendanceSecurity';

// ASSUMPTION: response/request field names below follow the checklist where it names them
// (faculty_id, name, marked_today, window_open, days). Others (challenge_id, nonce, public_key,
// signature, ...) are guesses: check them against the Go handlers and edit the types here.

export type Challenge = { challenge_id: string; nonce: string };
export type RegisterResponse = { faculty_id: string; name?: string };
export type AttendanceResponse = { ok?: boolean };
export type HistoryResponse = {
  name: string;
  marked_today: boolean;
  window_open: boolean;
  days: string[]; // YYYY-MM-DD
  today?: string; // server's campus date (YYYY-MM-DD); sent by every server version
  // v2 (SRV-2). Absent when the server is older; the app then keeps the v1 flow.
  // PROVISIONAL names/formats (SERVER_PLAN.md SRV-2) until 00_OVERVIEW.md §5.2 is available.
  is_in?: boolean;                 // an IN session is open now
  open_since_ist?: string | null;  // when that session started, IST
  today_worked_s?: number;         // closed sessions today, seconds
  // Per-day status. Shape not known yet; src/lib/dayStatus.ts parseDayStatus() is the only reader.
  day_status?: unknown;
};

export type AttendanceArgs = {
  facultyId: string; challengeId: string; nonce: string;
  latMicro: number; lonMicro: number; accuracyCm: number; signature: string;
};

// ---- v2 (IN/OUT), see docs/PROTOCOL.md. Needs a server with SRV-2: an older server rejects the
// unknown `action` field with bad_request.
export type { AttendAction };
export type AttendanceV2Args = AttendanceArgs & { action: AttendAction };
// PROVISIONAL: field names from APP_PLAN.md APP-1 step 3; check against 00_OVERVIEW.md §5.1
// (not in the repo yet) and the SRV-2 handler.
export type AttendanceV2Response = {
  action: AttendAction;
  day: string;            // YYYY-MM-DD, campus time
  at_ist: string;         // time of this IN/OUT, IST
  distance_m: number;
  is_in: boolean;         // state after this action
  today_worked_s: number; // closed sessions today, seconds
};
export type HistoryArgs = { facultyId: string; challengeId: string; nonce: string; signature: string };

export type ClientOptions = { baseUrl: string; fetchImpl?: typeof fetch; timeoutMs?: number };

export function createClient({ baseUrl, fetchImpl, timeoutMs = 10_000 }: ClientOptions) {
  const isDev = typeof __DEV__ !== 'undefined' ? __DEV__ : true; // jest has no __DEV__
  if (!/^https?:\/\//i.test(baseUrl)) {
    throw new Error('API base URL must start with http:// or https://');
  }
  const allowHttp = process.env.EXPO_PUBLIC_ALLOW_HTTP === '1'; // demo builds only
  if (!isDev && !allowHttp && !/^https:\/\//i.test(baseUrl)) {
    throw new Error('API base URL must use https:// in release builds');
  }
  const root = baseUrl.replace(/\/+$/, '');
  const doFetch = fetchImpl ?? fetch;

  async function post<T>(path: string, body: unknown): Promise<T> {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), timeoutMs);
    let res: Response;
    try {
      res = await doFetch(`${root}${path}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
        signal: controller.signal,
      });
    } catch (e) {
      throw new ApiError((e as Error)?.name === 'AbortError' ? 'timeout' : 'network');
    } finally {
      clearTimeout(timer);
    }

    let data: unknown = null;
    try { data = await res.json(); } catch { /* non-JSON body */ }

    if (!res.ok) throw mapServerError(res.status, data);
    if (data === null || typeof data !== 'object') throw new ApiError('bad_response', res.status);
    return data as T;
  }

  return {
    register: (code: string, publicKey: string) =>
      post<RegisterResponse>('/register', { code, public_key: publicKey }),
    challenge: (facultyId: string) =>
      post<Challenge>('/challenge', { faculty_id: facultyId }),
    attendance: (a: AttendanceArgs) =>
      post<AttendanceResponse>('/attendance', {
        faculty_id: a.facultyId, challenge_id: a.challengeId,
        lat_micro: a.latMicro, lon_micro: a.lonMicro, accuracy_cm: a.accuracyCm, signature: a.signature,
      }),
    // Same path; the server picks v2 when `action` is present. The nonce is not sent: the server
    // rebuilds the signed string from the nonce it stored for the challenge.
    attendanceV2: (a: AttendanceV2Args) =>
      post<AttendanceV2Response>('/attendance', {
        action: a.action, faculty_id: a.facultyId, challenge_id: a.challengeId,
        lat_micro: a.latMicro, lon_micro: a.lonMicro, accuracy_cm: a.accuracyCm, signature: a.signature,
      }),
    history: (a: HistoryArgs) =>
      post<HistoryResponse>('/history', {
        faculty_id: a.facultyId, challenge_id: a.challengeId, signature: a.signature,
      }),
  };
}

export type ApiClient = ReturnType<typeof createClient>;

let shared: ApiClient | null = null;
/** App-wide client; reads EXPO_PUBLIC_API_BASE_URL on first use. */
export function api(): ApiClient {
  if (!shared) {
    const baseUrl = process.env.EXPO_PUBLIC_API_BASE_URL;
    if (!baseUrl) throw new Error('EXPO_PUBLIC_API_BASE_URL is not set');
    shared = createClient({ baseUrl });
  }
  return shared;
}
