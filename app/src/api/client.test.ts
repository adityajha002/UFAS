import { createClient } from './client';
import { ApiError } from './errors';

const BASE = 'https://example.test/api';
const reply = (status: number, body: unknown) =>
  jest.fn(async () => ({ ok: status >= 200 && status < 300, status, json: async () => body }) as unknown as Response);

describe('createClient', () => {
  it('rejects base URLs that are not http(s)', () => {
    expect(() => createClient({ baseUrl: 'ftp://example.test' })).toThrow(/http/);
  });

  it('allows http in development', () => {
    expect(() => createClient({ baseUrl: 'http://192.168.1.10:8080' })).not.toThrow();
  });

  it('posts JSON and returns the parsed body', async () => {
    const f = reply(200, { challenge_id: 'c1', nonce: 'n1' });
    const c = createClient({ baseUrl: BASE + '/', fetchImpl: f });
    expect(await c.challenge('f1')).toEqual({ challenge_id: 'c1', nonce: 'n1' });
    expect(f).toHaveBeenCalledWith(`${BASE}/challenge`, expect.objectContaining({ method: 'POST' }));
  });

  it('attendanceV2 sends action and the v1 fields, without the nonce', async () => {
    const res = { action: 'out', day: '2026-11-10', at_ist: '2026-11-10T17:05:00+05:30', distance_m: 12, is_in: false, today_worked_s: 28800 };
    const f = reply(201, res);
    const c = createClient({ baseUrl: BASE, fetchImpl: f });
    const out = await c.attendanceV2({
      action: 'out', facultyId: 'f1', challengeId: 'c1', nonce: 'n1',
      latMicro: 28606827, lonMicro: 77209000, accuracyCm: 1500, signature: 'SIG',
    });
    expect(out).toEqual(res);
    const [url, init] = (f.mock.calls[0] as unknown as [string, RequestInit]);
    expect(url).toBe(`${BASE}/attendance`);
    expect(JSON.parse(init.body as string)).toEqual({
      action: 'out', faculty_id: 'f1', challenge_id: 'c1',
      lat_micro: 28606827, lon_micro: 77209000, accuracy_cm: 1500, signature: 'SIG',
    });
  });

  it('v1 attendance body has no action', async () => {
    const f = reply(201, { day: '2026-11-10', distance_m: 12 });
    const c = createClient({ baseUrl: BASE, fetchImpl: f });
    await c.attendance({ facultyId: 'f1', challengeId: 'c1', nonce: 'n1', latMicro: 1, lonMicro: 2, accuracyCm: 3, signature: 'S' });
    const body = JSON.parse((f.mock.calls[0] as unknown as [string, RequestInit])[1].body as string);
    expect(body).not.toHaveProperty('action');
    expect(body).not.toHaveProperty('nonce');
  });

  it.each([
    'closed', 'outside_area', 'poor_accuracy', 'already_marked', 'already_in', 'not_in', 'challenge_expired',
    'challenge_used', 'challenge_invalid', 'too_many_challenges', 'not_enrolled',
    'code_invalid', 'bad_signature', 'bad_request',
  ])('maps server error %s', async (code) => {
    const c = createClient({ baseUrl: BASE, fetchImpl: reply(400, { error: code }) });
    await expect(c.challenge('f1')).rejects.toMatchObject({ code });
  });

  it.each([
    ['already_in', "You're already marked IN."],
    ['not_in', "You're not marked IN."],
  ])('%s has its own message, not "attendance already marked"', async (code, message) => {
    const c = createClient({ baseUrl: BASE, fetchImpl: reply(409, { error: code }) });
    const err = await c.challenge('f1').catch((e) => e);
    expect(err).toMatchObject({ code, status: 409, message });
    expect(err.message).not.toMatch(/attendance/i);
  });

  it('maps a 500 without a known code to server', async () => {
    const c = createClient({ baseUrl: BASE, fetchImpl: reply(500, { error: 'boom' }) });
    await expect(c.challenge('f1')).rejects.toMatchObject({ code: 'server', status: 500 });
  });

  it('maps fetch failure to network', async () => {
    const f = jest.fn(async () => { throw new TypeError('Network request failed'); });
    const c = createClient({ baseUrl: BASE, fetchImpl: f as unknown as typeof fetch });
    await expect(c.challenge('f1')).rejects.toMatchObject({ code: 'network' });
  });

  it('retries with fallback base URL on transport failure', async () => {
    const f = jest.fn()
      .mockImplementationOnce(async () => { throw new TypeError('Network request failed'); })
      .mockImplementationOnce(async () => ({
        ok: true,
        status: 200,
        json: async () => ({ challenge_id: 'c2', nonce: 'n2' }),
      }) as Response);
    const c = createClient({
      baseUrl: 'http://192.168.1.10:8080',
      fallbackBaseUrls: ['http://10.0.2.2:8080'],
      fetchImpl: f as unknown as typeof fetch,
    });

    await expect(c.challenge('f1')).resolves.toEqual({ challenge_id: 'c2', nonce: 'n2' });
    expect(f).toHaveBeenCalledTimes(2);
    expect((f.mock.calls[0] as unknown as [string])[0]).toBe('http://192.168.1.10:8080/challenge');
    expect((f.mock.calls[1] as unknown as [string])[0]).toBe('http://10.0.2.2:8080/challenge');
  });

  it('does not retry fallback on HTTP error responses', async () => {
    const f = jest.fn(async () => ({ ok: false, status: 400, json: async () => ({ error: 'bad_request' }) }) as Response);
    const c = createClient({
      baseUrl: 'http://192.168.1.10:8080',
      fallbackBaseUrls: ['http://10.0.2.2:8080'],
      fetchImpl: f as unknown as typeof fetch,
    });

    await expect(c.challenge('f1')).rejects.toMatchObject({ code: 'bad_request' });
    expect(f).toHaveBeenCalledTimes(1);
  });

  it('maps an abort to timeout', async () => {
    const f = jest.fn((_u: string, init: RequestInit) => new Promise((_res, rej) => {
      init.signal!.addEventListener('abort', () => rej(Object.assign(new Error('aborted'), { name: 'AbortError' })));
    }));
    const c = createClient({ baseUrl: BASE, fetchImpl: f as unknown as typeof fetch, timeoutMs: 20 });
    const err = await c.challenge('f1').catch((e) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err.code).toBe('timeout');
  });
});
