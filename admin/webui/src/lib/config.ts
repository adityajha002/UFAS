import type { AttendanceWindowItem, ConfigField } from "../types";

export const TIME_RE = /^(?:(?:[01]\d|2[0-3]):[0-5]\d|24:00)$/;

function parseMinutes(raw: string): number {
  if (raw === "24:00") return 24 * 60;
  const [h, m] = raw.split(":").map(Number);
  return h * 60 + m;
}

export function validateWindowsList(list: AttendanceWindowItem[], otherIds: Set<string>): string | null {
  const seen = new Set<string>();
  const parsed: { id: string; start: number; end: number }[] = [];
  for (const w of list) {
    const id = w.id.trim();
    const start = w.start.trim();
    const end = w.end.trim();
    if (!id) return "Every window needs an ID";
    if (seen.has(id) || otherIds.has(id)) return `Duplicate window ID: ${id}`;
    seen.add(id);
    if (!TIME_RE.test(start) || !TIME_RE.test(end)) return "Use HH:MM (or 24:00 for end)";
    if (start === "24:00") return "Start time cannot be 24:00";
    const sMin = parseMinutes(start);
    const eMin = parseMinutes(end);
    if (sMin >= eMin) return `Window "${id}": start must be before end`;
    parsed.push({ id, start: sMin, end: eMin });
  }
  for (let i = 0; i < parsed.length; i++) {
    for (let j = i + 1; j < parsed.length; j++) {
      if (parsed[i].start < parsed[j].end && parsed[j].start < parsed[i].end) {
        return `Windows overlap: ${parsed[i].id} and ${parsed[j].id}`;
      }
    }
  }
  return null;
}

export function windowsEqual(a: AttendanceWindowItem[], b: AttendanceWindowItem[]): boolean {
  return JSON.stringify(a) === JSON.stringify(b);
}

/** Turns the edited rows into the YAML-shaped body the server expects, and
 *  collects client-side problems per key. Only changed, editable keys are sent. */
export function buildPatch(fields: ConfigField[], draft: Record<string, string | AttendanceWindowItem[]>) {
  const patch: Record<string, Record<string, unknown>> = {};
  const errors: Record<string, string> = {};
  for (const f of fields) {
    if (!f.editable || !(f.key in draft)) continue;
    if (f.type === "windows") {
      const list = (draft[f.key] as AttendanceWindowItem[]).map((w) => ({
        id: w.id.trim(), start: w.start.trim(), end: w.end.trim(),
      }));
      if (windowsEqual(list, (f.value as AttendanceWindowItem[]) ?? [])) continue;
      const otherIds = new Set<string>();
      for (const other of fields) {
        if (other.type === "windows" && other.key !== f.key) {
          const otherList = (draft[other.key] as AttendanceWindowItem[] | undefined) ?? (other.value as AttendanceWindowItem[] | undefined) ?? [];
          for (const w of otherList) if (w.id.trim()) otherIds.add(w.id.trim());
        }
      }
      const err = validateWindowsList(list, otherIds);
      if (err) { errors[f.key] = err; continue; }
      const [section, name] = f.key.split(".");
      (patch[section] ??= {})[name] = list;
      continue;
    }
    const raw = String(draft[f.key]).trim();
    if (raw === String(f.value)) continue;
    let v: number | string;
    if (f.type === "time") {
      if (!TIME_RE.test(raw)) { errors[f.key] = "Use HH:MM, for example 09:30 (24:00 means end of day)"; continue; }
      v = raw;
    } else {
      const n = Number(raw);
      if (raw === "" || !Number.isFinite(n)) { errors[f.key] = "Enter a number"; continue; }
      if (f.type === "int" && !Number.isInteger(n)) { errors[f.key] = "Enter a whole number"; continue; }
      if (f.min !== undefined && n < f.min) { errors[f.key] = `Must be at least ${f.min}`; continue; }
      if (f.max !== undefined && n > f.max) { errors[f.key] = `Must be at most ${f.max}`; continue; }
      v = n;
    }
    const [section, name] = f.key.split(".");
    (patch[section] ??= {})[name] = v;
  }
  return { patch, errors, empty: Object.keys(patch).length === 0 };
}

function fmtChangeVal(v: unknown): string {
  if (Array.isArray(v)) {
    if (v.length === 0) return "none";
    return v.map((w: AttendanceWindowItem) => `${w.start}-${w.end}`).join(", ");
  }
  return String(v);
}

export function describeChanges(changed?: Record<string, { old: unknown; new: unknown }>): string {
  if (!changed) return "";
  return Object.entries(changed).map(([k, c]) => `${k.split(".").pop()} ${fmtChangeVal(c.old)} → ${fmtChangeVal(c.new)}`).join(", ");
}
