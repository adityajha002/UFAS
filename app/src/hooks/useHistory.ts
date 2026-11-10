import { useCallback, useEffect, useRef, useState } from 'react';
import { AppState } from 'react-native';
import { api, AttendanceV2Response, HistoryResponse } from '../api/client';
import native from '../native/attendanceSecurity';
import { loadHistory, saveHistory } from '../lib/storage';
import { fetchHistory } from '../lib/fetchHistory';
import { classifyHistoryError, HistoryStatus, historyErrorMessage } from '../lib/historyView';

const deps = {
  challenge: (id: string) => api().challenge(id),
  signHistory: (f: { challengeId: string; nonce: string; facultyId: string }) => native.signHistory(f),
  history: (a: { facultyId: string; challengeId: string; nonce: string; signature: string }) => api().history(a),
};

const FOREGROUND_MIN_GAP_MS = 15_000; // every history call uses a server challenge; don't spam

export function useHistory(facultyId: string) {
  const [data, setData] = useState<HistoryResponse | null>(null);
  const [savedAt, setSavedAt] = useState<number | null>(null);
  const [fresh, setFresh] = useState(false); // data came from the server in this session
  const [status, setStatus] = useState<HistoryStatus>('loading');
  const [error, setError] = useState<string | null>(null);
  const [pulling, setPulling] = useState(false);

  const inflight = useRef(false);
  const rerun = useRef(false);
  const lastAttempt = useRef(0);
  const alive = useRef(true);

  const refresh = useCallback(async (pull = false) => {
    if (inflight.current) { rerun.current = true; return; } // e.g. a mark finished mid-fetch
    inflight.current = true;
    lastAttempt.current = Date.now();
    if (pull) setPulling(true);
    try {
      const res = await fetchHistory(facultyId, deps);
      const now = Date.now();
      saveHistory(facultyId, res).catch(() => {});
      if (!alive.current) return;
      setData(res); setSavedAt(now); setFresh(true); setStatus('ready'); setError(null);
    } catch (e) {
      if (!alive.current) return;
      setStatus(classifyHistoryError(e));
      setError(historyErrorMessage(e));
    } finally {
      inflight.current = false;
      if (alive.current) setPulling(false);
      if (rerun.current) { rerun.current = false; refresh(); }
    }
  }, [facultyId]);

  // Show the cache straight away, then fetch.
  useEffect(() => {
    alive.current = true;
    (async () => {
      const cached = await loadHistory(facultyId).catch(() => null);
      if (alive.current && cached) { setData(cached.data); setSavedAt(cached.savedAt); }
      refresh();
    })();
    return () => { alive.current = false; };
  }, [refresh]);

  useEffect(() => {
    const sub = AppState.addEventListener('change', (st) => {
      if (st === 'active' && Date.now() - lastAttempt.current > FOREGROUND_MIN_GAP_MS) refresh();
    });
    return () => sub.remove();
  }, [refresh]);

  /**
   * After an IN/OUT call: apply the server's reply at once (so the button flips without waiting),
   * then re-fetch /history, which replaces it. With no reply (already_in / not_in) just re-fetch.
   */
  const afterMark = useCallback((res?: AttendanceV2Response) => {
    if (res && alive.current) {
      setData((d) => (d ? {
        ...d,
        is_in: res.is_in,
        today_worked_s: res.today_worked_s,
        open_since_ist: res.is_in ? res.at_ist : null,
      } : d));
    }
    refresh();
  }, [refresh]);

  return { data, savedAt, fresh, status, error, pulling, refresh: () => refresh(), pull: () => refresh(true), afterMark };
}
