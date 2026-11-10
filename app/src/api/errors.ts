export type ApiErrorCode =
  | 'closed' | 'outside_area' | 'poor_accuracy' | 'already_marked'
  | 'already_in' | 'not_in' | 'request_pending' | 'out_window_open'
  | 'challenge_expired' | 'challenge_used' | 'too_many_challenges'
  | 'not_enrolled' | 'code_invalid' | 'bad_signature'
  | 'bad_request' | 'challenge_invalid'
  | 'network' | 'timeout' | 'server' | 'bad_response' | 'unknown';

const MESSAGES: Record<ApiErrorCode, string> = {
  closed: 'Attendance is closed right now.',
  outside_area: 'You appear to be outside the campus area.',
  poor_accuracy: 'Your location is not accurate enough. Move somewhere with a clearer sky and try again.',
  already_marked: 'Your attendance is already marked for today.', // v1 only
  already_in: "You're already marked IN.",
  not_in: "You're not marked IN.",
  request_pending: 'An early-exit request is already pending for today.',
  out_window_open: 'The OUT window is open now. Use MARK OUT instead.',
  challenge_expired: 'The request took too long. Please try again.',
  challenge_used: 'That request was already used. Please try again.',
  too_many_challenges: 'Too many requests. Wait a minute and try again.',
  not_enrolled: 'This device is not enrolled. Please visit the office.',
  code_invalid: 'That enrolment code is invalid, expired or already used. Ask the office for a new code.',
  bad_signature: 'This device could not be verified. Re-enrol, or visit the office.',
  bad_request: 'The app sent an invalid request. Please update the app or contact the office.',
  challenge_invalid: 'That request was not recognised. Please try again.',
  network: 'No connection to the server. Check your internet and try again.',
  timeout: 'The server took too long to respond. Try again.',
  server: 'The server had a problem. Try again shortly.',
  bad_response: 'Unexpected response from the server.',
  unknown: 'Something went wrong. Try again.',
};

const SERVER_CODES = new Set<string>([
  'closed', 'outside_area', 'poor_accuracy', 'already_marked', 'already_in', 'not_in',
  'request_pending', 'out_window_open', 'challenge_expired', 'challenge_used', 'challenge_invalid', 'too_many_challenges',
  'not_enrolled', 'code_invalid', 'bad_signature', 'bad_request',
]);

/** The user-facing text for a code (same text an ApiError with that code carries). */
export const messageFor = (code: ApiErrorCode): string => MESSAGES[code];

export class ApiError extends Error {
  constructor(public code: ApiErrorCode, public status: number = 0) {
    super(MESSAGES[code]);
    this.name = 'ApiError';
  }
}

/** Maps an HTTP status and the server's `{error: "..."}` body to an ApiError. */
export function mapServerError(status: number, body: unknown): ApiError {
  const code = (body as { error?: unknown } | null)?.error;
  if (typeof code === 'string' && SERVER_CODES.has(code)) return new ApiError(code as ApiErrorCode, status);
  if (status >= 500) return new ApiError('server', status);
  return new ApiError('unknown', status);
}
