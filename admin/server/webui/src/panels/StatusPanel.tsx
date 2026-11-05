import { useState } from "react";
import { api, ApiError } from "../api";
import { EmployeeInput } from "../components/dialogs";
import { errText, useApp } from "../ctx";
import { fmtBytes, fmtUptime, timePart } from "../lib/dates";
import { copyText } from "../lib/download";

const REASONS: Record<string, string> = {
  open: "open now", before_start: "closed, not open yet today", after_end: "closed for today", weekend: "closed, weekend",
};

export function StatusPanel() {
  const { status: s, down, stopped, openModal, requestAuditFilter, toast, notify } = useApp();
  const w = s.attendance_window;

  // command inputs
  const [addId, setAddId] = useState("");
  const [addName, setAddName] = useState("");
  const [addErr, setAddErr] = useState<Record<string, string>>({});
  const [addBusy, setAddBusy] = useState(false);
  const [codeId, setCodeId] = useState("");
  const [revId, setRevId] = useState("");
  const [markId, setMarkId] = useState("");

  async function add(e: React.FormEvent) {
    e.preventDefault();
    setAddBusy(true); setAddErr({});
    try {
      const r = await api.addFaculty(addId.trim(), addName.trim());
      toast(`Added ${r.name} (${r.employee_id}). Create an enrollment code next.`);
      setAddId(""); setAddName(""); notify();
    } catch (x) {
      if (x instanceof ApiError && x.fields) setAddErr(x.fields);
      else setAddErr({ form: errText(x) });
    } finally { setAddBusy(false); }
  }

  return (
    <section className="panel top" aria-label="Server status">
      <div className="panel-head">
        <h2>Server status</h2>
        <button className="icon-btn power" onClick={() => openModal({ kind: "shutdown" })} disabled={down || stopped} aria-label="Turn off the server" title="Turn off the server">⏻</button>
      </div>
      <div className="panel-body">
        <div className="status-line" aria-live="polite">
          <span className={down ? "state down" : "state up"}>{stopped && down ? "STOPPED" : down ? "DOWN" : stopped ? "STOPPING" : "RUNNING"}</span>
          {!down && <span className="muted">up {fmtUptime(s.uptime_s)}</span>}
          <span className={w.open ? "chip ok" : "chip"} title={`${REASONS[w.reason] ?? w.reason}, ${w.start}–${w.end}`}>window {w.open ? "open" : "closed"}</span>
        </div>

        {s.network.warning && <p className="banner warn">{s.network.warning}</p>}
        {s.config_warnings.length > 0 && <ul className="banner warn">{s.config_warnings.map((m) => <li key={m}>{m}</li>)}</ul>}

        <h3 className="sub sr-only">Commands</h3>
        <form className="cmd" onSubmit={add}>
          <div className="cmd-inputs">
            <EmployeeInputPlain value={addId} onChange={setAddId} label="New employee id" err={addErr.employee_id} />
            <input aria-label="Name" value={addName} onChange={(e) => setAddName(e.target.value)} placeholder="Full name" maxLength={100} />
            {addErr.name && <p className="field-err">{addErr.name}</p>}
            {(addErr.form) && <p className="field-err">{addErr.form}</p>}
          </div>
          <button className="btn cmd-btn" disabled={down || addBusy || !addId.trim() || !addName.trim()}>Add faculty</button>
        </form>
        <div className="cmd">
          <div className="cmd-inputs"><label className="sr-only" htmlFor="c-code">Employee id</label><EmployeeInput id="c-code" value={codeId} onChange={setCodeId} /></div>
          <button className="btn cmd-btn" disabled={down || !codeId.trim()} onClick={() => openModal({ kind: "code", employee_id: codeId.trim() })}>Enrollment code</button>
        </div>
        <div className="cmd">
          <div className="cmd-inputs"><label className="sr-only" htmlFor="c-rev">Employee id</label><EmployeeInput id="c-rev" value={revId} onChange={setRevId} /></div>
          <button className="btn cmd-btn" disabled={down || !revId.trim()} onClick={() => openModal({ kind: "revoke", employee_id: revId.trim() })}>Revoke faculty</button>
        </div>
        <div className="cmd">
          <div className="cmd-inputs"><label className="sr-only" htmlFor="c-mark">Employee id</label><EmployeeInput id="c-mark" value={markId} onChange={setMarkId} /></div>
          <button className="btn cmd-btn" disabled={down || !markId.trim()} onClick={() => openModal({ kind: "mark", employee_id: markId.trim() })}>Mark manually</button>
        </div>
        <h3 className="sub">Details</h3>
        <dl className="kv">
          <dt>Window</dt>
          <dd><span className={w.open ? "chip ok" : "chip"}>{w.open ? "OPEN" : "CLOSED"}</span> {REASONS[w.reason] ?? w.reason}, {w.start}–{w.end}{w.next_change_at_ist && ` · changes ${timePart(w.next_change_at_ist).slice(0, 5)}`}</dd>
          <dt>Phone URL</dt>
          <dd>
            {s.network.phone_urls.length === 0 && <span className="muted">none found</span>}
            {s.network.phone_urls.map((u) => (
              <span key={u} className="url"><code>{u}</code> <button className="link" onClick={async () => toast((await copyText(u)) ? "URL copied" : "Copy failed", "ok")}>Copy</button></span>
            ))}
          </dd>
          <dt>Network</dt>
          <dd>{s.network.hostname}{s.network.addresses.map((a) => <span key={a.ip} className="addr">{a.interface} {a.ip}</span>)}
            <span className="muted"> · phone port {s.public_listener.reachable ? "reachable ✓" : "not reachable ✗"}</span></dd>
          <dt>Database</dt>
          <dd>{s.database.ok ? "ok ✓" : <span className="field-err">problem ✗</span>} · {fmtBytes(s.database.size_bytes)}</dd>
          <dt>People</dt>
          <dd>{s.counts.faculty} faculty · {s.counts.enrolled} enrolled · {s.counts.marked_today} marked today</dd>
          <dt>Last 24 h</dt>
          <dd>{s.attendance_attempts_24h.ok} ok ·{" "}
            {s.attendance_attempts_24h.failed > 0
              ? <button className="link bad" onClick={() => requestAuditFilter("failed")}>{s.attendance_attempts_24h.failed} failed</button>
              : "0 failed"}</dd>
        </dl>

        <p className="muted small">To rotate this panel’s login token, run <code>server admin-token</code> on the server host and restart. A stopped server cannot be started from a web page: run <code>server serve</code> on the host.</p>
      </div>
    </section>
  );
}

function EmployeeInputPlain({ value, onChange, label, err }: { value: string; onChange: (v: string) => void; label: string; err?: string }) {
  return (
    <>
      <input aria-label={label} value={value} onChange={(e) => onChange(e.target.value)} placeholder={label} maxLength={64} autoComplete="off" />
      {err && <p className="field-err">{err}</p>}
    </>
  );
}
