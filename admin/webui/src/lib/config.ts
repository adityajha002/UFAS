import type { ConfigField } from "../types";

export const TIME_RE = /^(?:(?:[01]\d|2[0-3]):[0-5]\d|24:00)$/;

/** Turns the edited rows into the YAML-shaped body the server expects, and
 *  collects client-side problems per key. Only changed, editable keys are sent. */
export function buildPatch(fields: ConfigField[], draft: Record<string, string>) {
  const patch: Record<string, Record<string, number | string>> = {};
  const errors: Record<string, string> = {};
  for (const f of fields) {
    if (!f.editable || !(f.key in draft)) continue;
    const raw = draft[f.key].trim();
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

export function describeChanges(changed?: Record<string, { old: unknown; new: unknown }>): string {
  if (!changed) return "";
  return Object.entries(changed).map(([k, c]) => `${k.split(".").pop()} ${String(c.old)} → ${String(c.new)}`).join(", ");
}
