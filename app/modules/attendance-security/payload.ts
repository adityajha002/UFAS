// The strings the device key signs, in TypeScript. No imports, so Jest can test them.
//
// On Android the Kotlin module builds these itself (PayloadBuilder.kt) and JS only passes fields;
// this file is used by the software fallback in index.ts and by the golden-vector tests.
// Both MUST match the server's crypto.go byte for byte (docs/PROTOCOL.md).

export type AttendAction = 'in' | 'out';

export type AttendanceFields = {
  challengeId: string;
  nonce: string;
  facultyId: string;
  latMicro: number;
  lonMicro: number;
  accuracyCm: number;
};

export type AttendanceV2Fields = AttendanceFields & { action: AttendAction };

export type HistoryFields = {
  challengeId: string;
  nonce: string;
  facultyId: string;
};

export const ATTEND_ACTIONS: readonly AttendAction[] = ['in', 'out'];

export const attendV1Payload = (f: AttendanceFields): string =>
  `attend.v1|${f.challengeId}|${f.nonce}|${f.facultyId}|${f.latMicro}|${f.lonMicro}|${f.accuracyCm}`;

/** Throws Error('bad_action') unless action is "in" or "out" (same rule as the Kotlin module). */
export function attendV2Payload(f: AttendanceV2Fields): string {
  if (!ATTEND_ACTIONS.includes(f.action)) throw new Error('bad_action');
  return `attend.v2|${f.action}|${f.challengeId}|${f.nonce}|${f.facultyId}|${f.latMicro}|${f.lonMicro}|${f.accuracyCm}`;
}

export const historyPayload = (f: HistoryFields): string =>
  `history.v1|${f.challengeId}|${f.nonce}|${f.facultyId}`;
