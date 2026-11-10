import type {
  ActionPage, AuditPage, CodeIssued, ConfigBody, ConfigHistoryItem, Day, ExitRequestDetail, FacultyList,
  Status, UserAttendance,
} from "./types";

const BASE = "/admin/api";
const MAIN_SERVER_URL = "http://127.0.0.1:8080";
const KEY = "usict_admin_token";
const TIMEOUT_MS = 10_000;

export class ApiError extends Error {
  constructor(
    public status: number, public code: string, message: string,
    public fields?: Record<string, string>, public retryAfter?: number,
  ) { super(message); }
}
export class NetworkError extends Error {}

// ---- token: sessionStorage only, never in a URL ----
let token: string | null = null;
try { token = sessionStorage.getItem(KEY); } catch { /* storage blocked: stay in memory */ }

export const getToken = () => token;
export function setToken(t: string | null) {
  token = t;
  try { t ? sessionStorage.setItem(KEY, t) : sessionStorage.removeItem(KEY); } catch { /* ignore */ }
}
let onUnauthorized: (() => void) | null = null;
export function setUnauthorizedHandler(fn: (() => void) | null) { onUnauthorized = fn; }

type Query = Record<string, string | number | boolean | null | undefined>;
function qs(q?: Query): string {
  if (!q) return "";
  const p = new URLSearchParams();
  for (const [k, v] of Object.entries(q)) if (v !== undefined && v !== null && v !== "") p.set(k, String(v));
  const s = p.toString();
  return s ? `?${s}` : "";
}

async function toApiError(res: Response): Promise<ApiError> {
  let body: { error?: string; message?: string; fields?: Record<string, string> } = {};
  try { body = await res.json(); } catch { /* not JSON */ }
  const ra = Number(res.headers.get("Retry-After"));
  return new ApiError(res.status, body.error ?? `http_${res.status}`, body.message ?? `HTTP ${res.status}`,
    body.fields, Number.isFinite(ra) && ra > 0 ? ra : undefined);
}

async function send(method: string, path: string, opts: { query?: Query; body?: unknown; token?: string | null } = {}): Promise<Response> {
  const ctl = new AbortController();
  const timer = setTimeout(() => ctl.abort(), TIMEOUT_MS);
  const t = opts.token === undefined ? token : opts.token;
  const headers: Record<string, string> = {};
  if (t) headers.Authorization = `Bearer ${t}`;
  if (opts.body !== undefined) headers["Content-Type"] = "application/json";
  try {
    return await fetch(BASE + path + qs(opts.query), {
      method, headers, signal: ctl.signal, cache: "no-store",
      body: opts.body === undefined ? undefined : JSON.stringify(opts.body),
    });
  } catch {
    throw new NetworkError("Cannot reach the server");
  } finally {
    clearTimeout(timer);
  }
}

async function call<T>(method: string, path: string, opts: { query?: Query; body?: unknown } = {}): Promise<T> {
  const res = await send(method, path, opts);
  if (!res.ok) {
    const err = await toApiError(res);
    if (err.code === "unauthorized") onUnauthorized?.();
    throw err;
  }
  return (await res.json()) as T;
}

const enc = encodeURIComponent; // employee ids always go through this

export const api = {
  /** Login check: does not trigger the global 401 handler. */
  async login(candidate: string): Promise<Status> {
    const res = await send("GET", "/status", { token: candidate });
    if (!res.ok) throw await toApiError(res);
    setToken(candidate);
    return (await res.json()) as Status;
  },
  status: () => call<Status>("GET", "/status"),
  day: (date?: string) => call<Day>("GET", "/attendance/day", { query: { date } }),
  faculty: () => call<FacultyList>("GET", "/faculty"),
  userAttendance: (emp: string, from?: string, to?: string) =>
    call<UserAttendance>("GET", `/faculty/${enc(emp)}/attendance`, { query: { from, to } }),
  addFaculty: (employee_id: string, name: string) =>
    call<{ employee_id: string; name: string; enrolled: boolean }>("POST", "/faculty", { body: { employee_id, name } }),
  issueCode: (emp: string) => call<CodeIssued>("POST", `/faculty/${enc(emp)}/code`, { body: {} }),
  revoke: (emp: string) => call<{ employee_id: string; enrolled: boolean }>("POST", `/faculty/${enc(emp)}/revoke`, { body: {} }),
  deleteFaculty: (emp: string) => call<{ employee_id: string; deleted: boolean }>("DELETE", `/faculty/${enc(emp)}`),
  markManual: (employee_id: string, date: string, note: string) =>
    call<{ employee_id: string; date: string }>("POST", "/attendance/manual", { body: { employee_id, date, note } }),
  exitRequests: (q?: { date?: string; status?: "pending" | "approved" | "all" }) =>
    call<{ items: ExitRequestDetail[] }>("GET", "/exit-requests", { query: q }),
  exitRequest: (requestId: string) =>
    call<ExitRequestDetail>("GET", `/exit-requests/${enc(requestId)}`),
  approveExitRequest: (requestId: string) =>
    call<{
      id: string; employee_id: string; status: string;
      approved_at_ist: string; out_at_ist: string; session_s: number; today_worked_s: number; is_in: boolean;
    }>("POST", `/exit-requests/${enc(requestId)}/approve`, { body: {} }),
  config: () => call<ConfigBody>("GET", "/config"),
  patchConfig: (patch: unknown, dryRun = false) =>
    call<ConfigBody>("PATCH", "/config", { body: patch, query: { dry_run: dryRun ? "true" : undefined } }),
  resetOverride: (key: string) => call<ConfigBody>("DELETE", "/config/overrides", { query: { key } }),
  configHistory: (limit = 50) => call<{ items: ConfigHistoryItem[] }>("GET", "/config/history", { query: { limit } }),
  audit: (q: Query) => call<AuditPage>("GET", "/audit", { query: q }),
  actions: (q: Query) => call<ActionPage>("GET", "/actions", { query: q }),
  async shutdown(): Promise<{ status: string; at_ist: string }> {
    try {
      const res = await fetch(`${MAIN_SERVER_URL}/server/shutdown`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ confirm: "shutdown" }),
      });
      if (!res.ok) throw new Error(`main server shutdown rejected: ${res.status}`);
      return (await res.json()) as { status: string; at_ist: string };
    } catch {
      return await call<{ status: string; at_ist: string }>("POST", "/server/shutdown", { body: { confirm: "shutdown" } });
    }
  },

  /** CSV needs the bearer header, so it cannot be a plain <a href>. */
  async exportCsv(from: string, to: string, includeLocation: boolean): Promise<{ blob: Blob; filename: string }> {
    const res = await send("GET", "/export/attendance.csv", { query: { from, to, include_location: includeLocation ? "1" : undefined } });
    if (!res.ok) {
      const err = await toApiError(res); // errors are JSON, never saved as a file
      if (err.code === "unauthorized") onUnauthorized?.();
      throw err;
    }
    const filename = filenameFromDisposition(res.headers.get("Content-Disposition")) ?? `attendance_${from}_to_${to}.csv`;
    return { blob: await res.blob(), filename };
  },
};

export function filenameFromDisposition(h: string | null): string | null {
  if (!h) return null;
  const m = /filename="?([^";]+)"?/i.exec(h);
  return m ? m[1].replace(/[\\/]/g, "_") : null;
}
