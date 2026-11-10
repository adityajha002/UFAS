import { markAttendance, markAttendanceV2, MarkDeps, MarkV2Deps } from './markAttendance';
import { ApiError, ApiErrorCode } from '../api/errors';
import type { AttendanceV2Response } from '../api/client';
import { fixFromRaw, LocationError } from './fix';

const FIX = { latMicro: 28600000, lonMicro: 77200000, accuracyCm: 1500 };

function setup(attendanceResults: Array<ApiErrorCode | 'ok'>, clock = { t: 0 }) {
  const log: string[] = [];
  let challengeN = 0;
  const queue = [...attendanceResults];
  const d: MarkDeps = {
    getFix: async () => { log.push('fix'); return FIX; },
    challenge: async () => { log.push('challenge'); challengeN++; return { challenge_id: `c${challengeN}`, nonce: `n${challengeN}` }; },
    signAttendance: async (f) => { log.push(`sign:${f.challengeId}`); return { signature: 'SIG' }; },
    attendance: async (a) => {
      log.push(`attend:${a.challengeId}`);
      const r = queue.shift() ?? 'ok';
      if (r !== 'ok') throw new ApiError(r, 400);
      return {};
    },
    now: () => clock.t,
  };
  return { d, log, clock };
}

describe('markAttendance', () => {
  it('fix, challenge, sign, submit', async () => {
    const { d, log } = setup(['ok']);
    expect(await markAttendance('f1', d)).toEqual({ status: 'success' });
    expect(log).toEqual(['fix', 'challenge', 'sign:c1', 'attend:c1']);
  });

  it('poor_accuracy: new fix, same challenge', async () => {
    const { d, log } = setup(['poor_accuracy', 'ok']);
    expect(await markAttendance('f1', d)).toEqual({ status: 'success' });
    expect(log).toEqual(['fix', 'challenge', 'sign:c1', 'attend:c1', 'fix', 'sign:c1', 'attend:c1']);
  });

  it('gives up on poor_accuracy after 2 retries', async () => {
    const { d } = setup(['poor_accuracy', 'poor_accuracy', 'poor_accuracy', 'ok']);
    await expect(markAttendance('f1', d)).rejects.toMatchObject({ code: 'poor_accuracy' });
  });

  it('outside_area retries once then throws', async () => {
    const { d } = setup(['outside_area', 'outside_area', 'ok']);
    await expect(markAttendance('f1', d)).rejects.toMatchObject({ code: 'outside_area' });
  });

  it.each(['challenge_expired', 'challenge_invalid'] as const)('%s: fetches a new challenge', async (code) => {
    const { d, log } = setup([code, 'ok']);
    expect(await markAttendance('f1', d)).toEqual({ status: 'success' });
    expect(log.filter((x) => x === 'challenge')).toHaveLength(2);
    expect(log[log.length - 1]).toBe('attend:c2');
  });

  it('refreshes a challenge that is about to expire before reusing it', async () => {
    const { d, log, clock } = setup(['poor_accuracy', 'ok']);
    const orig = d.getFix;
    let n = 0;
    d.getFix = async () => { if (++n === 2) clock.t = 56_000; return orig(); };
    await markAttendance('f1', d);
    expect(log[log.length - 1]).toBe('attend:c2');
  });

  it('already_marked resolves, not throws', async () => {
    const { d } = setup(['already_marked']);
    expect(await markAttendance('f1', d)).toEqual({ status: 'already_marked' });
  });

  it.each(['closed', 'not_enrolled', 'bad_signature'] as const)('%s is thrown without retry', async (code) => {
    const { d, log } = setup([code, 'ok']);
    await expect(markAttendance('f1', d)).rejects.toMatchObject({ code });
    expect(log.filter((x) => x.startsWith('attend'))).toHaveLength(1);
  });

  it('location failure stops before any challenge is fetched', async () => {
    const { d, log } = setup(['ok']);
    d.getFix = async () => { throw new LocationError('mocked'); };
    await expect(markAttendance('f1', d)).rejects.toMatchObject({ code: 'mocked' });
    expect(log).toEqual([]);
  });
});

const V2_RES: AttendanceV2Response = {
  action: 'in', day: '2026-11-10', at_ist: '2026-11-10T09:12:00+05:30', distance_m: 40, is_in: true, today_worked_s: 0,
};

function setupV2(results: Array<ApiErrorCode | 'ok'>) {
  const log: string[] = [];
  let challengeN = 0;
  const queue = [...results];
  const d: MarkV2Deps = {
    getFix: async () => { log.push('fix'); return FIX; },
    challenge: async () => { log.push('challenge'); challengeN++; return { challenge_id: `c${challengeN}`, nonce: `n${challengeN}` }; },
    signAttendanceV2: async (f) => { log.push(`sign:${f.action}:${f.challengeId}:${f.nonce}:${f.facultyId}`); return { signature: 'SIG' }; },
    attendanceV2: async (a) => {
      log.push(`attend:${a.action}:${a.challengeId}:${a.signature}`);
      const r = queue.shift() ?? 'ok';
      if (r !== 'ok') throw new ApiError(r, 409);
      return { ...V2_RES, action: a.action, is_in: a.action === 'in' };
    },
    now: () => 0,
  };
  return { d, log };
}

describe('markAttendanceV2', () => {
  it.each(['in', 'out'] as const)('%s: fix, challenge, sign with action, submit with action', async (action) => {
    const { d, log } = setupV2(['ok']);
    const res = await markAttendanceV2('f1', action, d);
    expect(res).toEqual({ status: 'success', response: { ...V2_RES, action, is_in: action === 'in' } });
    expect(log).toEqual(['fix', 'challenge', `sign:${action}:c1:n1:f1`, `attend:${action}:c1:SIG`]);
  });

  it('reports stages in order', async () => {
    const { d } = setupV2(['ok']);
    const stages: string[] = [];
    await markAttendanceV2('f1', 'in', d, (s) => stages.push(s));
    expect(stages).toEqual(['locating', 'verifying']);
  });

  it.each(['already_in', 'not_in'] as const)('%s resolves without retry', async (code) => {
    const { d, log } = setupV2([code, 'ok']);
    expect(await markAttendanceV2('f1', code === 'already_in' ? 'in' : 'out', d)).toEqual({ status: code });
    expect(log.filter((x) => x.startsWith('attend'))).toHaveLength(1);
  });

  it('OUT poor_accuracy: new fix, same challenge', async () => {
    const { d, log } = setupV2(['poor_accuracy', 'ok']);
    expect(await markAttendanceV2('f1', 'out', d)).toMatchObject({ status: 'success' });
    expect(log).toEqual(['fix', 'challenge', 'sign:out:c1:n1:f1', 'attend:out:c1:SIG', 'fix', 'sign:out:c1:n1:f1', 'attend:out:c1:SIG']);
  });

  it('OUT outside_area retries once then throws', async () => {
    const { d } = setupV2(['outside_area', 'outside_area', 'ok']);
    await expect(markAttendanceV2('f1', 'out', d)).rejects.toMatchObject({ code: 'outside_area' });
  });

  it('challenge_used: fetches a new challenge', async () => {
    const { d, log } = setupV2(['challenge_used', 'ok']);
    expect(await markAttendanceV2('f1', 'in', d)).toMatchObject({ status: 'success' });
    expect(log[log.length - 1]).toBe('attend:in:c2:SIG');
  });

  it.each(['closed', 'bad_signature', 'bad_request', 'already_marked'] as const)('%s is thrown without retry', async (code) => {
    const { d, log } = setupV2([code, 'ok']);
    await expect(markAttendanceV2('f1', 'out', d)).rejects.toMatchObject({ code });
    expect(log.filter((x) => x.startsWith('attend'))).toHaveLength(1);
  });

  it('a signing failure (e.g. key_missing) is thrown before any submit', async () => {
    const { d, log } = setupV2(['ok']);
    d.signAttendanceV2 = async () => { throw new Error('Call to function failed: key_missing'); };
    await expect(markAttendanceV2('f1', 'in', d)).rejects.toThrow('key_missing');
    expect(log.some((x) => x.startsWith('attend'))).toBe(false);
  });
});

describe('fixFromRaw', () => {
  it('converts to micro-degrees and centimetres', () => {
    expect(fixFromRaw({ coords: { latitude: 28.6, longitude: 77.2, accuracy: 15 } })).toEqual(FIX);
  });
  it('refuses mocked fixes', () => {
    expect(() => fixFromRaw({ coords: { latitude: 1, longitude: 1, accuracy: 5 }, mocked: true })).toThrow();
  });
  it('rejects a missing accuracy', () => {
    expect(() => fixFromRaw({ coords: { latitude: 1, longitude: 1, accuracy: null } })).toThrow();
  });
});
