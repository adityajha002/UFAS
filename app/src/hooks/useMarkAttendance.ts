import { useCallback, useEffect, useRef, useState } from 'react';
import { ApiError } from '../api/errors';
import { api } from '../api/client';
import native from '../native/attendanceSecurity';
import { getFix } from '../lib/location';
import { LocationError, locationMessage } from '../lib/fix';
import { markAttendance, MarkDeps } from '../lib/markAttendance';
import type { MarkState } from '../lib/markState';

const deps: MarkDeps = {
  getFix: () => getFix(),
  challenge: (id) => api().challenge(id),
  signAttendance: (f) => native.signAttendance(f),
  attendance: (a) => api().attendance(a),
  now: () => Date.now(),
};

const BASE_MESSAGE: Partial<Record<MarkState, string>> = {
  closed: 'Attendance is not open right now',
  marked: 'Your attendance is marked for today',
};

/**
 * `base` is what the server last said (idle / closed / marked); section 7's history hook will
 * supply it. Until then it defaults to 'idle' and the server's own `closed` reply is the check.
 */
export function useMarkAttendance(opts: { facultyId: string; base?: MarkState; onMarked?: () => void }) {
  const { facultyId, base = 'idle', onMarked } = opts;
  const [override, setOverride] = useState<{ state: MarkState; message?: string } | null>(null);
  const lock = useRef(false);

  // A fresh server status (e.g. history refreshed) replaces any stale local result.
  useEffect(() => { setOverride(null); }, [base]);

  const mark = useCallback(async () => {
    if (lock.current) return; // no double taps
    lock.current = true;
    setOverride({ state: 'working', message: 'Getting your location…' });
    try {
      const res = await markAttendance(facultyId, deps, (stage) =>
        setOverride({ state: 'working', message: stage === 'locating' ? 'Getting your location…' : 'Verifying…' }));
      setOverride(res.status === 'success'
        ? { state: 'success', message: 'Attendance marked' }
        : { state: 'marked', message: 'Your attendance was already marked today' });
      onMarked?.();
    } catch (e) {
      if (e instanceof ApiError && e.code === 'closed') setOverride({ state: 'closed', message: e.message });
      else if (e instanceof ApiError || e instanceof LocationError) {
        setOverride({ state: 'error', message: e instanceof ApiError ? e.message : locationMessage(e.code) });
      } else setOverride({ state: 'error', message: 'Something went wrong. Try again.' });
    } finally {
      lock.current = false;
    }
  }, [facultyId, onMarked]);

  const reset = useCallback(() => setOverride(null), []);
  const state = override?.state ?? base;
  return { state, message: override ? override.message : BASE_MESSAGE[base], mark, reset };
}
