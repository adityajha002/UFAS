import { useCallback, useRef, useState } from 'react';
import { ApiError, messageFor } from '../api/errors';
import { api, AttendAction, AttendanceV2Response } from '../api/client';
import native from '../native/attendanceSecurity';
import { getFix } from '../lib/location';
import { LocationError, locationMessage } from '../lib/fix';
import { markAttendanceV2, MarkV2Deps } from '../lib/markAttendance';
import { successMessage } from '../lib/inOut';

const deps: MarkV2Deps = {
  getFix: () => getFix(),
  challenge: (id) => api().challenge(id),
  signAttendanceV2: (f) => native.signAttendanceV2(f),
  attendanceV2: (a) => api().attendanceV2(a),
  now: () => Date.now(),
};

const STAGE = { locating: 'Getting your location…', verifying: 'Verifying…' } as const;

function failureMessage(e: unknown): string {
  if (e instanceof ApiError) return e.message;
  if (e instanceof LocationError) return locationMessage(e.code);
  return 'Something went wrong. Try again.';
}

/**
 * IN/OUT (v2) marking. `action` comes from the server's last /history (null = not known yet).
 *  - success: shows "Marked IN at …" / "Marked OUT — worked …", then onDone(response)
 *  - already_in / not_in: the screen was stale; shows the message, then onDone() to refresh. No retry.
 *  - any other failure (closed, outside_area, poor_accuracy, network…): shows the message;
 *    the IN/OUT state is unchanged.
 * The result message stays until the next tap.
 */
export function useMarkInOut(opts: {
  facultyId: string;
  action: AttendAction | null;
  onDone: (res?: AttendanceV2Response) => void;
}) {
  const { facultyId, action, onDone } = opts;
  const [stage, setStage] = useState<string | null>(null); // non-null while a request runs
  const [notice, setNotice] = useState<string | null>(null);
  const lock = useRef(false);

  const mark = useCallback(async () => {
    if (lock.current || !action) return; // no double taps; nothing to send until the state is known
    lock.current = true;
    setNotice(null);
    setStage(STAGE.locating);
    try {
      const r = await markAttendanceV2(facultyId, action, deps, (st) => setStage(STAGE[st]));
      if (r.status === 'success') {
        setNotice(successMessage(r.response));
        onDone(r.response);
      } else {
        setNotice(messageFor(r.status));
        onDone();
      }
    } catch (e) {
      setNotice(failureMessage(e));
    } finally {
      lock.current = false;
      setStage(null);
    }
  }, [facultyId, action, onDone]);

  return { busy: stage !== null, stage, notice, mark };
}
