import { requireNativeModule } from 'expo-modules-core';
import {
  attendRequestOutPayload, attendV1Payload, attendV2Payload, historyPayload,
  type AttendAction, type AttendanceFields, type AttendanceV2Fields, type HistoryFields,
  type RequestOutAction, type RequestOutFields,
} from './payload';

export type { AttendAction, AttendanceFields, AttendanceV2Fields, HistoryFields, RequestOutAction, RequestOutFields };
export { attendRequestOutPayload, attendV2Payload, ATTEND_ACTIONS, REQUEST_OUT_ACTION } from './payload';

export type SecurityLevel = 'strongbox' | 'trusted_environment' | 'software';
export type KeyInfo = { securityLevel: SecurityLevel };

export interface AttendanceSecurity {
  hasKey(): Promise<boolean>;
  getKeyInfo(): Promise<KeyInfo | null>;
  generateKey(): Promise<{ publicKey: string; securityLevel: SecurityLevel }>;
  /** v1 (single daily mark). Kept as the rollback path. */
  signAttendance(f: AttendanceFields): Promise<{ signature: string }>;
  /** v2 (IN/OUT). Rejects any action other than "in"/"out". */
  signAttendanceV2(f: AttendanceV2Fields): Promise<{ signature: string }>;
  /** Special early-exit request using the same challenge/location flow as attendance. */
  signRequestOut(f: RequestOutFields): Promise<{ signature: string }>;
  signHistory(f: HistoryFields): Promise<{ signature: string }>;
  deleteKey(): Promise<void>;
}

const toBase64 = (bytes: ArrayBuffer): string => {
  let binary = '';
  const view = new Uint8Array(bytes);
  for (let i = 0; i < view.byteLength; i++) binary += String.fromCharCode(view[i]);
  return globalThis.btoa(binary);
};

let keyPair: CryptoKeyPair | null = null;

async function ensureKeyPair(): Promise<CryptoKeyPair> {
  if (keyPair) return keyPair;
  const subtle = globalThis.crypto?.subtle;
  if (!subtle) throw new Error('WebCrypto is unavailable in this environment.');
  keyPair = await subtle.generateKey({ name: 'ECDSA', namedCurve: 'P-256' }, true, ['sign', 'verify']);
  return keyPair;
}

async function softSign(message: string): Promise<{ signature: string }> {
  const kp = await ensureKeyPair();
  const sig = await globalThis.crypto.subtle.sign({ name: 'ECDSA', hash: 'SHA-256' }, kp.privateKey, new TextEncoder().encode(message));
  return { signature: toBase64(sig) };
}

const fallback: AttendanceSecurity = {
  async hasKey() { return !!(await ensureKeyPair().catch(() => null)); },
  async getKeyInfo() {
    const kp = await ensureKeyPair().catch(() => null);
    return kp ? { securityLevel: 'software' } : null;
  },
  async generateKey() {
    const kp = await ensureKeyPair();
    const publicKey = await globalThis.crypto.subtle.exportKey('spki', kp.publicKey);
    return { publicKey: toBase64(publicKey), securityLevel: 'software' as const };
  },
  async signAttendance(f) { return softSign(attendV1Payload(f)); },
  async signAttendanceV2(f) { return softSign(attendV2Payload(f)); },
  async signRequestOut(f) { return softSign(attendRequestOutPayload(f)); },
  async signHistory(f) { return softSign(historyPayload(f)); },
  async deleteKey() { keyPair = null; },
};

let native: AttendanceSecurity;
try {
  native = requireNativeModule<AttendanceSecurity>('AttendanceSecurity');
} catch {
  native = fallback;
}

export default native;
