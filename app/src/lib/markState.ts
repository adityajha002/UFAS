// 'unknown' (v2 only): IN/OUT state not known yet (no /history from the server this session).
export type MarkState = 'idle' | 'closed' | 'marked' | 'working' | 'success' | 'error' | 'unknown';
