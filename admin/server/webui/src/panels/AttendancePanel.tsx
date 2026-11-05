import { useState } from "react";
import { api } from "../api";
import { errText, useApp } from "../ctx";
import { usePolling } from "../hooks/usePolling";
import { addDays, fmtDay, isYmd, timePart } from "../lib/dates";
import type { Day, UserAttendance } from "../types";

export function AttendancePanel({ selected, onBack }: { selected: string | null; onBack: () => void }) {
  const [date, setDate] = useState<string | null>(null); // null = today; survives user mode
  const [showAbsent, setShowAbsent] = useState(false);
  return selected
    ? <UserMode emp={selected} onBack={onBack} />
    : <DayMode date={date} setDate={setDate} showAbsent={showAbsent} setShowAbsent={setShowAbsent} />;
}

function DayMode({ date, setDate, showAbsent, setShowAbsent }: {
  date: string | null; setDate: (d: string | null) => void; showAbsent: boolean; setShowAbsent: (b: boolean) => void;
}) {
  const { today, down, version, openModal } = useApp();
  const cur = date ?? today;
  const [day, setDay] = useState<Day | null>(null);
  const [err, setErr] = useState("");

  usePolling(async () => {
    try { setDay(await api.day(date ?? undefined)); setErr(""); }
    catch (e) { setErr(errText(e)); }
  }, cur === today ? 15_000 : 600_000, !down, null, [cur, version]);

  const go = (d: string) => setDate(d >= today ? null : d);
  const shown = day && day.date === cur ? day : null;
  const present = shown?.faculty.filter((f) => f.marked) ?? [];
  const absent = shown?.faculty.filter((f) => !f.marked) ?? [];

  return (
    <section className="panel top" aria-label="Attendance">
      <div className="panel-head">
        <h2>{cur === today ? "Today’s attendance" : "Attendance"}</h2>
        <div className="datenav">
          <button className="icon-btn" onClick={() => go(addDays(cur, -1))} aria-label="Previous day">‹</button>
          <label className="datebtn">
            {fmtDay(cur)}
            <input type="date" value={cur} max={today} aria-label="Pick a date"
              onClick={(e) => { try { e.currentTarget.showPicker?.(); } catch { /* ignore */ } }}
              onChange={(e) => { if (isYmd(e.target.value)) go(e.target.value); }} />
          </label>
          <button className="icon-btn" disabled={cur >= today} onClick={() => go(addDays(cur, 1))} aria-label="Next day">›</button>
          {cur !== today && <button className="btn small" onClick={() => setDate(null)}>Today</button>}
        </div>
      </div>
      <div className="panel-body">
        {err && <p className="field-err" role="alert">{err}</p>}
        {!shown && !err && <p className="muted">Loading…</p>}
        {shown && (
          <>
            <div className="count-line">
              <strong>{shown.summary.marked} present</strong>
              <span className="muted"> of {shown.summary.total}</span>
              {shown.summary.not_enrolled > 0 && <span className="chip">{shown.summary.not_enrolled} not enrolled</span>}
            </div>
            {shown.is_future && <p className="muted">Future date, nothing to show.</p>}
            {shown.is_weekend && !shown.is_future && <p className="note">Weekend, the attendance window is closed.</p>}
            {!shown.is_future && present.length === 0 && <p className="muted">Nobody has been marked present on this day.</p>}
            <ul className="rows">
              {present.map((f) => (
                <li key={f.employee_id} className="pill-row">
                  <span className="name" title={f.employee_id}>{f.name}</span>
                  {f.source === "manual"
                    ? <span className="chip" title={`Entered at ${f.marked_at_ist ? timePart(f.marked_at_ist) : "?"}${f.note ? `: ${f.note}` : ""}`}>manual</span>
                    : <span className="time">{f.marked_at_ist ? timePart(f.marked_at_ist) : ""}</span>}
                </li>
              ))}
            </ul>
            {!shown.is_future && absent.length > 0 && (
              <>
                <button className="link" onClick={() => setShowAbsent(!showAbsent)} aria-expanded={showAbsent}>
                  {showAbsent ? "Hide" : "Show"} {absent.length} not marked
                </button>
                {showAbsent && (
                  <ul className="rows absent">
                    {absent.map((f) => (
                      <li key={f.employee_id} className="pill-row dim">
                        <span className="name" title={f.employee_id}>{f.name}{!f.enrolled && <span className="chip">not enrolled</span>}</span>
                        <button className="link" onClick={() => openModal({ kind: "mark", employee_id: f.employee_id, date: cur })}>Mark present</button>
                      </li>
                    ))}
                  </ul>
                )}
              </>
            )}
          </>
        )}
      </div>
    </section>
  );
}

function UserMode({ emp, onBack }: { emp: string; onBack: () => void }) {
  const { down, version, today } = useApp();
  const [data, setData] = useState<UserAttendance | null>(null);
  const [err, setErr] = useState("");
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const rangeOk = (!from && !to) || (isYmd(from) && isYmd(to) && from <= to);

  usePolling(async () => {
    if (!rangeOk) return;
    try { setData(await api.userAttendance(emp, from || undefined, to || undefined)); setErr(""); }
    catch (e) { setErr(errText(e)); }
  }, 600_000, !down, null, [emp, from, to, version]); // refetches on window focus too

  const e = data?.employee;
  return (
    <section className="panel top" aria-label="User attendance">
      <div className="panel-head">
        <button className="btn small" onClick={onBack}>‹ Back</button>
        <h2 className="grow">{e ? `${e.name} (${e.employee_id})` : emp}</h2>
        {e && <span className={e.enrolled ? "dot on" : "dot"} title={e.enrolled ? "Phone enrolled" : "No phone enrolled"} aria-label={e.enrolled ? "Enrolled" : "Not enrolled"} />}
      </div>
      <div className="panel-body">
        {err && <p className="field-err" role="alert">{err}</p>}
        {!data && !err && <p className="muted">Loading…</p>}
        <div className="range-row">
          <label>From <input type="date" value={from} max={today} onChange={(x) => setFrom(x.target.value)} /></label>
          <label>to <input type="date" value={to} max={today} onChange={(x) => setTo(x.target.value)} /></label>
          {(from || to) && <button className="link" onClick={() => { setFrom(""); setTo(""); }}>Clear</button>}
        </div>
        {!rangeOk && <p className="field-err">Set both dates, with From not after To.</p>}
        {data && (
          <>
            <p className="stats">
              <strong>{data.days_present} days present</strong>
              <span className="muted"> · {data.via_phone} by phone, {data.manual} manual
                {data.first_marked_day && ` · first ${data.first_marked_day}, last ${data.last_marked_day}`}</span>
            </p>
            {data.days_truncated && <p className="note">Showing the latest 366 days. Narrow the range to see older days.</p>}
            {data.days.length === 0 && <p className="muted">No attendance recorded in this range.</p>}
            <ul className="rows">
              {data.days.map((d) => (
                <li key={d.day} className="pill-row" title={d.note || undefined}>
                  <span className="name">{fmtDay(d.day)}</span>
                  <span className="meta">
                    {d.source === "manual" ? <span className="chip">manual</span> : <span className="time">{timePart(d.marked_at_ist)}</span>}
                    {d.distance_m != null && <span className="muted"> {Math.round(d.distance_m)} m</span>}
                  </span>
                </li>
              ))}
            </ul>
          </>
        )}
      </div>
    </section>
  );
}
