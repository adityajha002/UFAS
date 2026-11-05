import { useEffect, useRef, useState } from "react";
import { api } from "../api";
import { errText, useApp } from "../ctx";
import { usePolling } from "../hooks/usePolling";
import { addDays, datePart, timePart } from "../lib/dates";
import { copyText } from "../lib/download";
import type { ActionItem, AuditItem } from "../types";

const UNVERIFIED = new Set(["bad_request", "challenge_invalid", "challenge_expired", "challenge_used", "bad_signature", "not_enrolled", "code_invalid"]);
const ACTION_LABEL: Record<string, string> = {
  faculty_add: "Added faculty", code_issue: "Enrollment code issued", faculty_revoke: "Revoked phone key",
  manual_mark: "Manual attendance", config_change: "Configuration changed", config_reset: "Configuration reset", shutdown: "Server shutdown",
};
const PAGE = 50;
type Filters = { outcome: string; endpoint: string; employee_id: string };
const NO_FILTER: Filters = { outcome: "", endpoint: "", employee_id: "" };

function mergeUnique(a: AuditItem[], b: AuditItem[]): AuditItem[] {
  const seen = new Set(a.map((x) => x.id));
  return [...a, ...b.filter((x) => !seen.has(x.id))].sort((x, y) => y.id - x.id);
}

export function AuditPanel() {
  const { today, down, version, auditPreset } = useApp();
  const [tab, setTab] = useState<"requests" | "actions">("requests");
  const [filters, setFilters] = useState<Filters>(NO_FILTER);
  const [filtersOpen, setFiltersOpen] = useState(false);
  const [older, setOlder] = useState(0); // each step widens the range by 7 days
  const [items, setItems] = useState<AuditItem[]>([]);
  const [nextBefore, setNextBefore] = useState<number | null>(null);
  const [loaded, setLoaded] = useState(false);
  const [err, setErr] = useState("");
  const [open, setOpen] = useState<number | null>(null);
  const gen = useRef(0);
  const busy = useRef(false);
  const itemsRef = useRef<AuditItem[]>([]);
  itemsRef.current = items;

  const from = addDays(today, -6 - 7 * older);
  const q = (extra: Record<string, string | number | undefined>) => ({
    from, to: today, outcome: filters.outcome || undefined, endpoint: filters.endpoint || undefined,
    employee_id: filters.employee_id.trim() || undefined, ...extra,
  });
  const fkey = JSON.stringify(filters);

  useEffect(() => { // "failed" link in the status panel
    if (!auditPreset) return;
    setTab("requests"); setFilters({ ...NO_FILTER, outcome: auditPreset.outcome }); setFiltersOpen(true);
  }, [auditPreset]);

  useEffect(() => { // new filters: start over
    gen.current++; itemsRef.current = []; setItems([]); setNextBefore(null); setLoaded(false); setOlder(0);
  }, [fkey]);

  // First page, then only what is newer than the newest row we have.
  usePolling(async () => {
    if (tab !== "requests") return;
    const g = gen.current;
    try {
      const newest = itemsRef.current[0]?.id;
      const page = await api.audit(q(newest ? { after_id: newest, limit: 200 } : { limit: PAGE }));
      if (g !== gen.current) return;
      setItems((cur) => mergeUnique(cur, page.items));
      if (!newest) setNextBefore(page.next_before_id);
      setLoaded(true); setErr("");
    } catch (e) { if (g === gen.current) setErr(errText(e)); }
  }, 5_000, !down && tab === "requests", null, [fkey, version, tab]);

  async function loadMore(widen = false) {
    if (busy.current) return;
    busy.current = true;
    const g = gen.current;
    const f = widen ? addDays(today, -6 - 7 * (older + 1)) : from;
    try {
      const last = itemsRef.current[itemsRef.current.length - 1];
      const page = await api.audit({ ...q({ before_id: last?.id, limit: PAGE }), from: f });
      if (g !== gen.current) return;
      if (widen) setOlder((o) => o + 1);
      setItems((cur) => mergeUnique(cur, page.items));
      setNextBefore(page.next_before_id);
    } catch (e) { setErr(errText(e)); } finally { busy.current = false; }
  }

  const onScroll = (e: React.UIEvent<HTMLDivElement>) => {
    const el = e.currentTarget;
    if (nextBefore != null && el.scrollTop + el.clientHeight >= el.scrollHeight - 80) void loadMore();
  };

  const rangeLabel = older === 0 ? "last 7 days" : `last ${7 * (older + 1)} days`;
  const filtered = filters.outcome || filters.endpoint || filters.employee_id;

  return (
    <section className="audit" aria-label="Audit">
      <div className="audit-head">
        <h2>Audit</h2>
        <div className="tabs" role="tablist">
          <button role="tab" aria-selected={tab === "requests"} className={tab === "requests" ? "on" : ""} onClick={() => setTab("requests")}>Phone requests</button>
          <button role="tab" aria-selected={tab === "actions"} className={tab === "actions" ? "on" : ""} onClick={() => setTab("actions")}>Admin actions</button>
        </div>
        {tab === "requests" && <button className="link" onClick={() => setFiltersOpen(!filtersOpen)} aria-expanded={filtersOpen}>{filtersOpen ? "Hide filters" : filtered ? "Filters on" : "Filters"}</button>}
      </div>
      {tab === "requests" && filtersOpen && (
        <div className="filters">
          <label>Outcome
            <select value={filters.outcome} onChange={(e) => setFilters({ ...filters, outcome: e.target.value })}>
              <option value="">All</option><option value="ok">OK</option><option value="failed">Any failure</option>
              {["outside_area", "poor_accuracy", "closed", "already_marked", "bad_signature", "not_enrolled", "challenge_expired", "code_invalid"].map((o) => <option key={o} value={o}>{o}</option>)}
            </select></label>
          <label>Request
            <select value={filters.endpoint} onChange={(e) => setFilters({ ...filters, endpoint: e.target.value })}>
              <option value="">All</option><option value="attendance">attendance</option><option value="challenge">challenge</option><option value="register">register</option><option value="history">history</option>
            </select></label>
          <label>Employee id <input value={filters.employee_id} onChange={(e) => setFilters({ ...filters, employee_id: e.target.value })} /></label>
          {filtered && <button className="link" onClick={() => setFilters(NO_FILTER)}>Clear</button>}
        </div>
      )}
      {tab === "requests" ? (
        <div className="scroll audit-list" onScroll={onScroll}>
          {err && <p className="field-err" role="alert">{err}</p>}
          {!loaded && !err && <p className="muted">Loading…</p>}
          {loaded && items.length === 0 && <p className="muted">{filtered ? "No requests match these filters." : `No phone requests in the ${rangeLabel}.`}</p>}
          <ul className="rows">
            {items.map((it) => <AuditRow key={it.id} it={it} today={today} open={open === it.id} toggle={() => setOpen(open === it.id ? null : it.id)} />)}
          </ul>
          {loaded && items.length > 0 && nextBefore == null && (
            <div className="list-end">End of the {rangeLabel}. <button className="link" onClick={() => void loadMore(true)}>Load older (previous 7 days)</button></div>
          )}
        </div>
      ) : <ActionsList />}
    </section>
  );
}

function AuditRow({ it, today, open, toggle }: { it: AuditItem; today: string; open: boolean; toggle: () => void }) {
  const { toast } = useApp();
  const ok = it.outcome === "ok";
  const who = it.faculty?.employee_id ? `${it.faculty.name ?? ""} (${it.faculty.employee_id})` : it.faculty ? "unknown id" : "unknown";
  const hasLoc = it.lat != null && it.lon != null;
  return (
    <li className={open ? "audit-row open" : "audit-row"}>
      <button className="audit-main" onClick={toggle} aria-expanded={open}>
        <span className="a-time">{datePart(it.at_ist) === today ? "" : <small>{datePart(it.at_ist).slice(5)}</small>}{timePart(it.at_ist)}</span>
        <span className="a-who">{who}</span>
        <span className="a-end">{it.endpoint}</span>
        <span className={ok ? "chip ok" : "chip bad"}>{ok ? "ok" : it.outcome}</span>
        <span className="a-dist">{it.distance_m != null ? `${Math.round(it.distance_m)} m` : ""}</span>
        <span className="a-ip">{it.remote_ip ?? ""}</span>
      </button>
      {open && (
        <div className="audit-detail">
          {hasLoc && !ok && UNVERIFIED.has(it.outcome) && <p className="note">Unverified location: these are the coordinates the caller claimed.</p>}
          <dl className="kv tight">
            {hasLoc && (<><dt>Location</dt><dd>{it.lat}, {it.lon}{it.accuracy_m != null && ` (±${Math.round(it.accuracy_m)} m)`}{" "}
              <button className="link" onClick={async () => toast((await copyText(`${it.lat}, ${it.lon}`)) ? "Coordinates copied" : "Copy failed")}>Copy</button>{" "}
              <a href={`https://www.google.com/maps?q=${it.lat},${it.lon}`} target="_blank" rel="noreferrer noopener">Open in maps</a></dd></>)}
            <dt>Result</dt><dd>HTTP {it.http_status}{it.duration_ms != null && `, ${it.duration_ms} ms`}</dd>
            {it.challenge_id && (<><dt>Challenge</dt><dd><code>{it.challenge_id}</code></dd></>)}
            {it.user_agent && (<><dt>Device</dt><dd className="wrap">{it.user_agent}</dd></>)}
            {it.policy && (<><dt>Rules then</dt><dd>radius {it.policy.radius_m} m, max error {it.policy.max_accuracy_m} m, window {it.policy.window_start}–{it.policy.window_end}</dd></>)}
          </dl>
        </div>
      )}
    </li>
  );
}

function detailText(d: unknown): string {
  if (!d || typeof d !== "object") return "";
  return Object.entries(d as Record<string, unknown>).map(([k, v]) => `${k}: ${typeof v === "object" ? JSON.stringify(v) : String(v)}`).join(" · ");
}

function ActionsList() {
  const { down, version } = useApp();
  const [items, setItems] = useState<ActionItem[] | null>(null);
  const [err, setErr] = useState("");
  usePolling(async () => {
    try { setItems((await api.actions({ limit: 100 })).items); setErr(""); } catch (e) { setErr(errText(e)); }
  }, 10_000, !down, null, [version]);
  return (
    <div className="scroll audit-list">
      {err && <p className="field-err" role="alert">{err}</p>}
      {!items && !err && <p className="muted">Loading…</p>}
      {items && items.length === 0 && <p className="muted">Nothing has been done from this panel yet.</p>}
      <ul className="rows">
        {items?.map((a) => (
          <li key={a.id} className="audit-row"><div className="audit-main static">
            <span className="a-time"><small>{a.at_ist.slice(5, 10)}</small>{timePart(a.at_ist)}</span>
            <span className="a-who">{ACTION_LABEL[a.action] ?? a.action}{a.target && a.target !== "all" ? ` · ${a.target}` : a.target === "all" ? " · all" : ""}</span>
            <span className="a-end wrap">{detailText(a.detail)}</span>
            <span className={a.outcome === "ok" ? "chip ok" : "chip bad"}>{a.outcome === "ok" ? "ok" : a.outcome}</span>
            <span className="a-dist" />
            <span className="a-ip">{a.remote_ip ?? ""}</span>
          </div></li>
        ))}
      </ul>
    </div>
  );
}
