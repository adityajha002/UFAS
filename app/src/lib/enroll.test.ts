import { enroll, enrollMessage, EnrollDeps, EnrollError, normalizeCode } from './enroll';
import { ApiError } from '../api/errors';

const CODE = '3fa91c07d2b84e6a15cd';

describe('normalizeCode', () => {
  it('accepts a bare code, any case, with stray whitespace', () => {
    expect(normalizeCode(` ${CODE.toUpperCase()}\n`)).toBe(CODE);
  });
  it('accepts grouped manual entry', () => {
    expect(normalizeCode('3fa9-1c07 d2b8-4e6a 15cd')).toBe(CODE);
  });
  it('extracts a standalone token from longer QR text', () => {
    expect(normalizeCode(`enroll:${CODE}`)).toBe(CODE);
  });
  it('accepts the code exactly as printed by the server CLI', () => {
    expect(normalizeCode(`${CODE}\n(valid 15 minutes, single use; show it as a QR)`)).toBe(CODE);
  });
  it.each(['', 'abc', 'zz'.repeat(10), CODE + 'a', '3fa91c07d2b84e6a15c'])('rejects %j', (v) => {
    expect(normalizeCode(v)).toBeNull();
  });

  it('explains when a 32-character faculty id is pasted instead of the 20-character code', () => {
    const facultyId = '96869b51760bce7e9f4103db830c6341';
    expect(normalizeCode(facultyId)).toBeNull();
    expect(enrollMessage(new EnrollError('invalid_format'))).toContain('faculty ID');
  });
});

function deps(over: Partial<EnrollDeps> = {}): EnrollDeps & { calls: string[] } {
  const calls: string[] = [];
  const d: EnrollDeps = {
    generateKey: async () => { calls.push('generate'); return { publicKey: 'PK' }; },
    deleteKey: async () => { calls.push('deleteKey'); },
    register: async () => { calls.push('register'); return { faculty_id: 'f1', name: 'Dr X' }; },
    saveEnrollment: async () => { calls.push('save'); },
    clearAll: async () => { calls.push('clearAll'); },
    ...over,
  };
  return Object.assign(d, { calls });
}

describe('enroll', () => {
  it('runs generate, register, save and returns the identity', async () => {
    const d = deps();
    expect(await enroll(CODE, d)).toEqual({ facultyId: 'f1', name: 'Dr X' });
    expect(d.calls).toEqual(['generate', 'register', 'save']);
  });

  it('rejects a malformed code before touching the key', async () => {
    const d = deps();
    await expect(enroll('nope', d)).rejects.toMatchObject({ code: 'invalid_format' });
    expect(d.calls).toEqual([]);
  });

  it('deletes the key and stored identity when register fails', async () => {
    const d = deps({ register: async () => { throw new ApiError('code_invalid', 400); } });
    await expect(enroll(CODE, d)).rejects.toMatchObject({ code: 'code_invalid' });
    expect(d.calls).toEqual(['generate', 'deleteKey', 'clearAll']);
  });

  it('cleans up when saving fails', async () => {
    const d = deps({ saveEnrollment: async () => { throw new Error('disk'); } });
    await expect(enroll(CODE, d)).rejects.toMatchObject({ code: 'save_failed' });
    expect(d.calls).toEqual(['generate', 'register', 'deleteKey', 'clearAll']);
  });

  it('reports key_failed and does not register', async () => {
    const d = deps({ generateKey: async () => { throw new Error('keystore'); } });
    await expect(enroll(CODE, d)).rejects.toMatchObject({ code: 'key_failed' });
    expect(d.calls).toEqual([]);
  });

  it('surfaces native-build requirements as a clear message', () => {
    const err = new Error('AttendanceSecurity native module unavailable. Use a native development build.');
    expect(enrollMessage(err)).toContain('native development build');
  });
});
