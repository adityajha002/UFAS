import { useEffect, useRef, useState } from "react";
import QRCode from "qrcode";
import { api, ApiError } from "../api";
import { Modal } from "./Modal";
import { errText, useApp } from "../ctx";
import { copyText } from "../lib/download";
import { fmtDay } from "../lib/dates";
import type { CodeIssued, ConfigHistoryItem } from "../types";

function Err({ text }: { text?: string }) {
  return text ? <p className="field-err" role="alert">{text}</p> : null;
}
export function EmployeeInput({ value, onChange, id }: { value: string; onChange: (v: string) => void; id: string }) {
  const { users } = useApp();
  return (
    <>
      <input id={id} list={`${id}-list`} value={value} onChange={(e) => onChange(e.target.value)} placeholder="Employee id" autoComplete="off" />
      <datalist id={`${id}-list`}>{users.map((u) => <option key={u.employee_id} value={u.employee_id}>{u.name}</option>)}</datalist>
    </>
  );
}

/* ---------- Enrollment code (CLI: code) ---------- */
export function CodeModal({ employeeId }: { employeeId: string }) {
  const { closeModal, notify, toast, users } = useApp();
  const [data, setData] = useState<CodeIssued | null>(null);
  const [error, setError] = useState("");
  const [qr, setQr] = useState("");
  const [left, setLeft] = useState(0);
  const asked = useRef(false);
  const name = users.find((u) => u.employee_id === employeeId)?.name;

  useEffect(() => {
    if (asked.current) return;
    asked.current = true;
    api.issueCode(employeeId).then((d) => {
      setData(d); setLeft(d.expires_in_s); notify();
      QRCode.toDataURL(d.code, { margin: 1, width: 220, errorCorrectionLevel: "M" }).then(setQr).catch(() => setQr(""));
    }).catch((e) => setError(errText(e)));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    if (!data) return;
    const end = performance.now() + data.expires_in_s * 1000; // duration only, never a date
    const t = setInterval(() => setLeft(Math.max(0, Math.round((end - performance.now()) / 1000))), 1000);
    return () => clearInterval(t);
  }, [data]);

  const expired = data !== null && left <= 0;
  return (
    <Modal title="Enrollment code" onClose={closeModal}>
      <p className="muted">{name ? `${name} (${employeeId})` : employeeId}</p>
      {error && <Err text={error} />}
      {!data && !error && <p>Creating code…</p>}
      {data && (
        <div className="code-box">
          <div className={expired ? "code expired" : "code"} aria-label="Enrollment code">{data.code}</div>
          {qr && !expired && <img className="qr" src={qr} alt="QR code of the enrollment code" width={220} height={220} />}
          <p className={expired ? "field-err" : "muted"} aria-live="polite">
            {expired ? "Expired. Close this and create a new code." : `Valid for ${Math.floor(left / 60)}:${String(left % 60).padStart(2, "0")} and works once.`}
          </p>
          <button className="btn" disabled={expired} onClick={async () => toast((await copyText(data.code)) ? "Code copied" : "Copy failed, select the code by hand", "ok")}>Copy code</button>
          <p className="muted small">This code is shown once. Closing this window discards it.</p>
        </div>
      )}
    </Modal>
  );
}

/* ---------- Manual mark (CLI: mark) ---------- */
export function MarkModal({ employeeId, date }: { employeeId?: string; date?: string }) {
  const { closeModal, notify, toast, today } = useApp();
  const [emp, setEmp] = useState(employeeId ?? "");
  const [day, setDay] = useState(date ?? today);
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const [fields, setFields] = useState<Record<string, string>>({});

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true); setErr(""); setFields({});
    try {
      await api.markManual(emp.trim(), day, note.trim());
      toast(`Marked ${emp.trim()} present on ${fmtDay(day)}`);
      notify(); closeModal();
    } catch (x) {
      if (x instanceof ApiError && x.fields) setFields(x.fields);
      setErr(errText(x));
    } finally { setBusy(false); }
  }
  return (
    <Modal title="Mark present manually" onClose={closeModal}>
      <form className="form" onSubmit={submit}>
        <label htmlFor="m-emp">Employee</label>
        <EmployeeInput id="m-emp" value={emp} onChange={setEmp} />
        <Err text={fields.employee_id} />
        <label htmlFor="m-date">Date</label>
        <input id="m-date" type="date" value={day} max={today} onChange={(e) => setDay(e.target.value)} />
        <Err text={fields.date} />
        <label htmlFor="m-note">Reason (required, kept in the record)</label>
        <input id="m-note" value={note} maxLength={200} onChange={(e) => setNote(e.target.value)} placeholder="For example: phone broken, verified by HoD" />
        <Err text={fields.note} />
        {err && !Object.keys(fields).length && <Err text={err} />}
        <div className="dialog-actions">
          <button type="button" className="btn ghost" onClick={closeModal}>Cancel</button>
          <button className="btn primary" disabled={busy || !emp.trim() || !note.trim() || !day}>Mark present</button>
        </div>
      </form>
    </Modal>
  );
}

function toMin(str: string | null | undefined): number | null {
  if (!str) return null;
  const s = str.includes("T") ? str.slice(11, 16) : str.slice(0, 5);
  if (!/^\d{2}:\d{2}$/.test(s)) return null;
  const [h, m] = s.split(":").map(Number);
  if (Number.isNaN(h) || Number.isNaN(m) || h < 0 || h > 23 || m < 0 || m > 59) return null;
  return h * 60 + m;
}

export function ExitDialog({ employeeId, date, inTime }: { employeeId: string; date: string; inTime?: string | null }) {
  const { closeModal, notify, toast, today, users } = useApp();
  const [outTime, setOutTime] = useState("");
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const [fields, setFields] = useState<Record<string, string>>({});
  const name = users.find((u) => u.employee_id === employeeId)?.name ?? employeeId;
  const opening = toMin(inTime ?? null);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true); setErr(""); setFields({});

    const timeOk = /^\d{2}:\d{2}$/.test(outTime);
    if (!timeOk) {
      setFields({ out_time: "Use HH:MM in 24-hour time" });
      setBusy(false);
      return;
    }
    const mins = toMin(outTime);
    if (mins == null) {
      setFields({ out_time: "Use HH:MM in 24-hour time" });
      setBusy(false);
      return;
    }
    if (date > today) {
      setFields({ date: "Date must not be in the future" });
      setBusy(false);
      return;
    }
    if (opening != null && date === today && mins < opening) {
      setFields({ out_time: "Exit time must be after the open session started" });
      setBusy(false);
      return;
    }
    if (date === today && mins > (new Date().getHours() * 60 + new Date().getMinutes()) + 1) {
      setFields({ out_time: "Exit time cannot be in the future" });
      setBusy(false);
      return;
    }
    if (!note.trim()) {
      setFields({ note: "Required" });
      setBusy(false);
      return;
    }

    try {
      await api.exit(employeeId, date, outTime, note.trim());
      toast(`Closed ${name}'s open session on ${fmtDay(date)}`);
      notify(); closeModal();
    } catch (x) {
      if (x instanceof ApiError && x.fields) setFields(x.fields);
      setErr(errText(x));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal title="Mark exit" onClose={closeModal}>
      <form className="form" onSubmit={submit}>
        <p><strong>{name}</strong> ({employeeId})</p>
        <p className="muted">Date: {fmtDay(date)}</p>
        {inTime && <p className="muted">Open session started at {toMin(inTime) != null ? inTime.slice(11, 16) : inTime}</p>}
        <label htmlFor="e-time">Exit time (HH:MM, required)</label>
        <input id="e-time" type="time" value={outTime} min={opening == null ? undefined : `${String(Math.floor(opening / 60)).padStart(2, "0")}:${String(opening % 60).padStart(2, "0")}`} max={date === today ? new Date().toTimeString().slice(0, 5) : undefined} onChange={(e) => setOutTime(e.target.value)} required />
        <Err text={fields.out_time} />
        <Err text={fields.date} />
        <label htmlFor="e-note">Note (required, kept in the record)</label>
        <input id="e-note" value={note} maxLength={200} onChange={(e) => setNote(e.target.value)} placeholder="For example: phone battery died, settled by admin" />
        <Err text={fields.note} />
        {err && !Object.keys(fields).length && <Err text={err} />}
        <div className="dialog-actions">
          <button type="button" className="btn ghost" onClick={closeModal}>Cancel</button>
          <button className="btn danger" disabled={busy || !outTime || !note.trim()}>{busy ? "Saving…" : "Mark exit"}</button>
        </div>
      </form>
    </Modal>
  );
}

/* ---------- Revoke (CLI: revoke) ---------- */
export function RevokeDialog({ employeeId }: { employeeId: string }) {
  const { closeModal, notify, toast, users } = useApp();
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const name = users.find((u) => u.employee_id === employeeId)?.name ?? employeeId;
  async function go() {
    setBusy(true); setErr("");
    try { await api.revoke(employeeId); toast(`Phone key cleared for ${name}`); notify(); closeModal(); }
    catch (x) { setErr(errText(x)); } finally { setBusy(false); }
  }
  return (
    <Modal title="Revoke faculty" onClose={closeModal}>
      <p>This clears the phone key for <strong>{name}</strong>. They must enroll again with a new code before they can mark attendance.</p>
      <Err text={err} />
      <div className="dialog-actions">
        <button className="btn ghost" onClick={closeModal}>Cancel</button>
        <button className="btn danger" disabled={busy} onClick={go}>Revoke phone key</button>
      </div>
    </Modal>
  );
}

export function DeleteDialog({ employeeId }: { employeeId: string }) {
  const { closeModal, notify, toast, users } = useApp();
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const name = users.find((u) => u.employee_id === employeeId)?.name ?? employeeId;
  async function go() {
    setBusy(true); setErr("");
    try {
      await api.deleteFaculty(employeeId);
      toast(`Deleted ${name} (${employeeId}) and cleared their attendance`);
      notify(); closeModal();
    } catch (x) {
      setErr(errText(x));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Modal title="Delete faculty" onClose={closeModal}>
      <p>This permanently removes <strong>{name}</strong> ({employeeId}) and clears their phone enrollment, attendance history, and challenge state. This cannot be undone.</p>
      <Err text={err} />
      <div className="dialog-actions">
        <button className="btn ghost" onClick={closeModal}>Cancel</button>
        <button className="btn danger" disabled={busy} onClick={go}>Delete faculty</button>
      </div>
    </Modal>
  );
}

/* ---------- Shutdown ---------- */
export function ShutdownDialog() {
  const { closeModal, markStopping, toast } = useApp();
  const [typed, setTyped] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  async function go(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true); setErr("");
    try { await api.shutdown(); markStopping(); toast("Server is stopping…"); closeModal(); }
    catch (x) { setErr(errText(x)); setBusy(false); }
  }
  return (
    <Modal title="Turn off the server" onClose={closeModal}>
      <form className="form" onSubmit={go}>
        <p>Phones will not be able to mark attendance until someone starts the server again on the host with <code>server serve</code>. This panel will also go offline.</p>
        <label htmlFor="sd">Type <code>shutdown</code> to confirm</label>
        <input id="sd" value={typed} onChange={(e) => setTyped(e.target.value)} autoComplete="off" />
        <Err text={err} />
        <div className="dialog-actions">
          <button type="button" className="btn ghost" onClick={closeModal}>Cancel</button>
          <button className="btn danger" disabled={busy || typed !== "shutdown"}>Turn off server</button>
        </div>
      </form>
    </Modal>
  );
}

/* ---------- Config history ---------- */
function Changes({ v }: { v: unknown }) {
  if (v && typeof v === "object") {
    const rows = Object.entries(v as Record<string, unknown>);
    return (
      <>{rows.map(([k, c]) => {
        const o = c as { old?: unknown; new?: unknown } | null;
        return <div key={k}>{k}: {o && typeof o === "object" && "new" in o ? `${String(o.old)} → ${String(o.new)}` : JSON.stringify(c)}</div>;
      })}</>
    );
  }
  return <>{String(v)}</>;
}
export function HistoryModal() {
  const { closeModal } = useApp();
  const [items, setItems] = useState<ConfigHistoryItem[] | null>(null);
  const [err, setErr] = useState("");
  useEffect(() => { api.configHistory(50).then((r) => setItems(r.items)).catch((e) => setErr(errText(e))); }, []);
  return (
    <Modal title="Configuration history" onClose={closeModal} wide>
      <Err text={err} />
      {!items && !err && <p>Loading…</p>}
      {items && items.length === 0 && <p className="muted">No changes saved yet.</p>}
      {items && items.length > 0 && (
        <div className="table-scroll">
          <table className="plain">
            <thead><tr><th>When (IST)</th><th>From</th><th>Change</th></tr></thead>
            <tbody>{items.map((h) => (
              <tr key={h.id}><td>{h.at_ist}</td><td>{h.remote_ip ?? "-"}</td><td><Changes v={h.changes} /></td></tr>
            ))}</tbody>
          </table>
        </div>
      )}
    </Modal>
  );
}
