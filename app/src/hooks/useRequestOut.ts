import { useCallback, useRef, useState } from 'react';
import { ApiError, messageFor } from '../api/errors';
import { api, RequestOutResponse } from '../api/client';
import native from '../native/attendanceSecurity';
import { getFix } from '../lib/location';
import { LocationError, locationMessage } from '../lib/fix';
import { requestOut, RequestOutDeps } from '../lib/requestOut';

const deps: RequestOutDeps = {
  getFix: () => getFix(),
  challenge: (id) => api().challenge(id),
  signRequestOut: (f) => native.signRequestOut(f),
  requestOut: (a) => api().requestOut(a),
  now: () => Date.now(),
};

const STAGE = { locating: 'Getting your location…', verifying: 'Verifying…' } as const;

function failureMessage(e: unknown): string {
  if (e instanceof ApiError) return e.message;
  if (e instanceof LocationError) return locationMessage(e.code);
  return 'Something went wrong. Try again.';
}

export function useRequestOut(opts: {
  facultyId: string;
  onDone: (res?: RequestOutResponse) => void;
}) {
  const { facultyId, onDone } = opts;
  const [stage, setStage] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const lock = useRef(false);

  const mark = useCallback(async () => {
    if (lock.current) return;
    lock.current = true;
    setNotice(null);
    setStage(STAGE.locating);
    try {
      const r = await requestOut(facultyId, deps, (st) => setStage(STAGE[st]));
      if (r.status === 'success') {
        setNotice('Exit request submitted — waiting for admin approval.');
        onDone(r.response);
      } else if (r.status === 'request_pending') {
        setNotice('You already have a pending exit request for today.');
        onDone();
      } else if (r.status === 'out_window_open') {
        setNotice('The OUT window is open now. Use MARK OUT instead.');
        onDone();
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
  }, [facultyId, onDone]);

  return { busy: stage !== null, stage, notice, mark };
}
