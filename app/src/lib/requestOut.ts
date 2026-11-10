import { ApiError } from '../api/errors';
import type { RequestOutResponse } from '../api/client';
import type { Fix } from './fix';

export const CHALLENGE_TTL_MS = 60_000;
const SAFETY_MS = 5_000;

export type RequestOutStage = 'locating' | 'verifying';
export type RequestOutResult =
  | { status: 'success'; response: RequestOutResponse }
  | { status: 'request_pending' | 'not_in' | 'out_window_open' };

type SignFields = {
  challengeId: string; nonce: string; facultyId: string;
  latMicro: number; lonMicro: number; accuracyCm: number;
};
type SubmitArgs = SignFields & { signature: string };

type FlowDeps = {
  getFix(): Promise<Fix>;
  challenge(facultyId: string): Promise<{ challenge_id: string; nonce: string }>;
  now(): number;
};

export type RequestOutDeps = FlowDeps & {
  signRequestOut(f: SignFields): Promise<{ signature: string }>;
  requestOut(a: SubmitArgs): Promise<RequestOutResponse>;
};

async function withRetries<T>(
  facultyId: string, d: FlowDeps, onStage: (s: RequestOutStage) => void,
  attempt: (fields: SignFields) => Promise<T>,
): Promise<T> {
  let ch: { id: string; nonce: string; at: number } | null = null;
  let accuracyRetries = 0, areaRetries = 0, renewals = 0;

  for (;;) {
    onStage('locating');
    const fix = await d.getFix();
    onStage('verifying');
    if (!ch || d.now() - ch.at > CHALLENGE_TTL_MS - SAFETY_MS) {
      const c = await d.challenge(facultyId);
      ch = { id: c.challenge_id, nonce: c.nonce, at: d.now() };
    }

    try {
      return await attempt({ challengeId: ch.id, nonce: ch.nonce, facultyId, ...fix });
    } catch (e) {
      if (!(e instanceof ApiError)) throw e;
      switch (e.code) {
        case 'poor_accuracy': if (accuracyRetries++ < 2) continue; throw e;
        case 'outside_area': if (areaRetries++ < 1) continue; throw e;
        case 'challenge_expired':
        case 'challenge_used':
        case 'challenge_invalid': if (renewals++ < 2) { ch = null; continue; } throw e;
        default: throw e;
      }
    }
  }
}

export async function requestOut(
  facultyId: string, d: RequestOutDeps, onStage: (s: RequestOutStage) => void = () => {},
): Promise<RequestOutResult> {
  try {
    const response = await withRetries(facultyId, d, onStage, async (fields) => {
      const { signature } = await d.signRequestOut(fields);
      return d.requestOut({ ...fields, signature });
    });
    return { status: 'success', response };
  } catch (e) {
    if (e instanceof ApiError) {
      if (e.code === 'request_pending') return { status: 'request_pending' };
      if (e.code === 'not_in') return { status: 'not_in' };
      if (e.code === 'out_window_open') return { status: 'out_window_open' };
    }
    throw e;
  }
}
