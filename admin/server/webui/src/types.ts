export type Status = {
  running: true; started_at: string; uptime_s: number; server_time_ist: string;
  public_listener: { configured: string; reachable: boolean; loopback_only: boolean };
  admin_listener: { configured: string };
  network: { hostname: string; addresses: { interface: string; ip: string }[]; phone_urls: string[]; warning: string };
  attendance_window: {
    open: boolean; reason: "open" | "before_start" | "after_end" | "weekend";
    start: string; end: string; next_change_at_ist: string | null; weekday: string;
  };
  database: { file: string; size_bytes: number | null; ok: boolean };
  counts: { faculty: number; enrolled: number; marked_today: number };
  attendance_attempts_24h: { ok: number; failed: number };
  config_warnings: string[];
};

export type Source = "phone" | "manual";

export type PendingExitRequest = {
  id: string;
  requested_at_ist: string;
  status: string;
};

export type StaleExitRequest = {
  id: string;
  employee_id: string;
  name: string;
  day: string;
  requested_at_ist: string;
  status: string;
};

export type DayItem = {
  employee_id: string; name: string; enrolled: boolean; marked: boolean;
  marked_at_ist: string | null; distance_m: number | null; source: Source | null; note: string;
  pending_exit_request?: PendingExitRequest | null;
};
export type Day = {
  date: string; is_today: boolean; is_weekend: boolean; is_future: boolean;
  summary: { total: number; marked: number; not_marked: number; not_enrolled: number };
  faculty: DayItem[];
  stale_exit_requests?: StaleExitRequest[];
};

export type ExitRequestDetail = {
  id: string;
  employee_id: string;
  name: string;
  day: string;
  requested_at: number;
  requested_at_ist: string;
  lat_micro: number;
  lon_micro: number;
  accuracy_cm: number;
  distance_m: number;
  status: string;
  approved_at_ist?: string | null;
  approved_by?: string | null;
  is_in?: boolean | null;
};

export type FacultyRow = {
  employee_id: string; name: string; enrolled: boolean; days_present: number;
  via_phone: number; manual: number; last_marked_day: string | null;
};
export type FacultyList = { range: { from: string | null; to: string | null }; total: number; faculty: FacultyRow[] };

export type UserDay = { day: string; marked_at_ist: string; source: Source; distance_m: number | null; note: string };
export type UserAttendance = {
  employee: { employee_id: string; name: string; enrolled: boolean };
  range: { from: string | null; to: string | null };
  days_present: number; via_phone: number; manual: number;
  first_marked_day: string | null; last_marked_day: string | null;
  days: UserDay[]; days_truncated: boolean;
};

export type ConfigField = {
  key: string; label: string; type: "float" | "time" | "string" | "int"; unit?: string;
  value: number | string; yaml_value?: number | string; source?: "yaml" | "override";
  editable: boolean; restart_required?: boolean; min?: number; max?: number;
};
export type ConfigBody = { fields: ConfigField[]; changed?: Record<string, { old: unknown; new: unknown }> };
export type ConfigHistoryItem = { id: number; ts: number; at_ist: string; remote_ip: string | null; changes: unknown };

export type AuditItem = {
  id: number; ts: number; at_ist: string; endpoint: "register" | "challenge" | "attendance" | "history";
  outcome: string; http_status: number;
  faculty: { id: string; employee_id: string | null; name: string | null } | null;
  challenge_id: string | null; lat: number | null; lon: number | null; accuracy_m: number | null;
  distance_m: number | null; remote_ip: string | null; user_agent: string | null; duration_ms: number | null;
  policy: { geo_lat: number; geo_lon: number; radius_m: number; max_accuracy_m: number; window_start: string; window_end: string } | null;
};
export type AuditPage = { items: AuditItem[]; next_before_id: number | null };

export type ActionItem = {
  id: number; ts: number; at_ist: string; action: string; target: string | null;
  detail: unknown; outcome: string; remote_ip: string | null;
};
export type ActionPage = { items: ActionItem[]; next_before_id: number | null };

export type CodeIssued = { employee_id: string; code: string; expires_in_s: number; expires_at_ist: string };

export type Modal =
  | { kind: "code"; employee_id: string }
  | { kind: "revoke"; employee_id: string }
  | { kind: "delete"; employee_id: string }
  | { kind: "mark"; employee_id?: string; date?: string }
  | { kind: "shutdown" }
  | { kind: "history" }
  | { kind: "exit_request"; request_id: string; employee_id: string; name: string };
