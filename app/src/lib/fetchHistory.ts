import type { HistoryResponse } from '../api/client';

export type HistoryDeps = {
  challenge(facultyId: string): Promise<{ challenge_id: string; nonce: string }>;
  signHistory(f: { challengeId: string; nonce: string; facultyId: string }): Promise<{ signature: string }>;
  history(a: { facultyId: string; challengeId: string; nonce: string; signature: string }): Promise<HistoryResponse>;
};

/** Each history call uses up one challenge: challenge -> sign -> history. */
export async function fetchHistory(facultyId: string, d: HistoryDeps): Promise<HistoryResponse> {
  const c = await d.challenge(facultyId);
  const f = { challengeId: c.challenge_id, nonce: c.nonce, facultyId };
  const { signature } = await d.signHistory(f);
  return d.history({ ...f, signature });
}
