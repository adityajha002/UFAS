import { useState } from "react";
import { api } from "../api";
import { errText, useApp } from "../ctx";
import { usePolling } from "../hooks/usePolling";
import { addDays, fmtDay, fmtHours, isYmd, timePart } from "../lib/dates";
import type { Day, DayItem, IncompleteItem, SessionItem, UserAttendance } from "../types";

export function AttendancePanel({ selected, onBack }: { selected: string | null; onBack: () => void }) {
  const [date, setDate] = useState<string | null>(null); // null = today; survives user mode
  const [showAbsent, setShowAbsent] = useState(false);
  return selected
    ? <UserMode emp={selected} onBack={onBack} />
    : <DayMode date={date} setDate={setDate} showAbsent={showAbsent} setShowAbsent={setShowAbsent} />;
}

function statusChip(status: DayItem["status"] | null | undefined) {
  switch (status) {
    case "IN_PROGRESS": return <span className="chip warn">IN</span>;
    case "INCOMPLETE": return <span className="chip bad">Incomplete</span>;
    default: return null;
  }
}

function totalHours(item: DayItem): number | null {
  if (typeof item.hours_s === "number") return item.hours_s;
  if (!Array.isArray(item.sessions)) return null;
  const total = item.sessions.reduce((sum, session) => sum + (typeof session.duration_s === "number" ? session.duration_s : 0), 0);
  return total > 0 ? total : null;
}

function displayRange(item: DayItem): string {
  const sessions = item.sessions ?? [];
  const firstIn = sessions.map((s) => s.in_ist).find((v): v is string => !!v) ?? null;
  const lastOut = [...sessions].reverse().map((s) => s.out_ist).find((v): v is string => !!v) ?? null;
  if (firstIn || lastOut) {
    return `${firstIn ? timePart(firstIn) : ""}${firstIn && lastOut ? " – " : ""}${lastOut ? timePart(lastOut) : ""}`;
  }
  return item.marked_at_ist ? timePart(item.marked_at_ist) : "";
}

function sessionRows(item: DayItem): SessionItem[] {
  return Array.isArray(item.sessions) ? item.sessions : [];
}

function DayMode({ date, setDate, showAbsent, setShowAbsent }: {
  date: string | null; setDate: (d: string | null) => void; showAbsent: boolean; setShowAbsent: (b: boolean) => void;
}) {
  const { today, down, version, openModal } = useApp();
  const cur = date ?? today;
  const [day, setDay] = useState<Day | null>(null);
  const [incompleteItems, setIncompleteItems] = useState<IncompleteItem[]>([]);
  const [showIncomplete, setShowIncomplete] = useState(false);
  const [err, setErr] = useState("");

  usePolling(async () => {
    try {
      setDay(await api.day(date ?? undefined));
      setIncompleteItems((await api.incomplete(date ?? undefined)).items);
      setErr("");
    } catch (e) {
      setErr(errText(e));
    }
  }, cur === today ? 15_000 : 600_000, !down, null, [cur, version]);

  const go = (d: string) => setDate(d >= today ? null : d);
  const shown = day && day.date === cur ? day : null;
  const present = shown?.faculty.filter((f) => f.marked) ?? [];
  const absent = shown?.faculty.filter((f) => !f.marked) ?? [];
  const incomplete = present.filter((f) => f.status === "INCOMPLETE");
  const inProgress = present.filter((f) => f.status === "IN_PROGRESS");
  const rest = present.filter((f) => f.status !== "INCOMPLETE" && f.status !== "IN_PROGRESS");
  const incompleteCount = shown?.summary.incomplete ?? incomplete.length;
  const inProgressCount = shown?.summary.in_progress ?? inProgress.length;
  const openInTime = (item: DayItem) => {
    const openSession = (item.sessions ?? []).find((s) => s.in_ist && !s.out_ist);
    return openSession?.in_ist ?? item.first_in_ist ?? item.marked_at_ist ?? null;
  };

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
              {inProgressCount > 0 && <span className="chip warn">{inProgressCount} in progress</span>}
              {incompleteCount > 0 && <span className="chip bad">{incompleteCount} incomplete</span>}
            </div>
            {shown.is_future && <p className="muted">Future date, nothing to show.</p>}
            {shown.is_weekend && !shown.is_future && <p className="note">Weekend, the attendance window is closed.</p>}
            {!shown.is_future && present.length === 0 && <p className="muted">Nobody has been marked present on this day.</p>}
            {(incompleteItems.length > 0 || incompleteCount > 0) && (
              <>
                <button className="link" onClick={() => setShowIncomplete(!showIncomplete)} aria-expanded={showIncomplete}>
                  {showIncomplete ? "Hide" : "Show"} all incomplete
                </button>
                {showIncomplete && (
                  <ul className="rows absent" aria-label="All incomplete">
                    {incompleteItems.length === 0 && <li className="muted">Nothing to fix.</li>}
                    {incompleteItems.map((item) => (
                      <li key={`${item.employee_id}-${item.date}-${item.in_ist}`} className="pill-row incomplete">
                        <span className="name" title={item.employee_id}>{item.date} · {item.name}</span>
                        <span className="meta">
                          <span className="time">{timePart(item.in_ist)}</span>
                          <button className="link" onClick={() => openModal({ kind: "exit", employee_id: item.employee_id, date: item.date, in_time: item.in_ist })}>Mark exit</button>
                        </span>
                      </li>
                    ))}
                  </ul>
                )}
              </>
            )}
            <ul className="rows">
              {incomplete.length > 0 && (
                <>
                  <li className="sub-row-label">Incomplete ({incomplete.length})</li>
                  {incomplete.map((f) => {
                    const inAt = openInTime(f);
                    const sessions = sessionRows(f);
                    const range = displayRange(f);
                    const hours = fmtHours(totalHours(f));
                    return (
                      <li key={f.employee_id} className="pill-row incomplete">
                        <div className="pill-row-stack">
                          <div className="pill-row-main">
                            <span className="name" title={f.employee_id}>{f.name}</span>
                            <span className="meta">
                              <span className="chip bad">Incomplete</span>
                              <span className="time">{inAt ? timePart(inAt) : range}</span>
                              <span className="hours">{hours}</span>
                            </span>
                          </div>
                          <ul className="session-list">
                            <li><span>{inAt ? `Open since ${timePart(inAt)}` : "Open session"}</span></li>
                            {sessions.length > 0 && sessions.map((session) => {
                              const inTime = session.in_ist ? timePart(session.in_ist) : "";
                              const outTime = session.out_ist ? timePart(session.out_ist) : "";
                              const source = session.in_source ?? session.out_source ?? session.source ?? null;
                              return (
                                <li key={`${session.in_ist ?? "open"}-${session.out_ist ?? "open"}-${source ?? "unknown"}`}>
                                  <span>{inTime && outTime ? `${inTime} → ${outTime}` : inTime || "Open session"}{typeof session.duration_s === "number" ? ` (${fmtHours(session.duration_s)})` : ""}</span>
                                  {source === "manual" && <span className="chip">manual</span>}
                                </li>
                              );
                            })}
                          </ul>
                        </div>
                        <button className="btn small danger" onClick={() => openModal({ kind: "exit", employee_id: f.employee_id, date: cur, in_time: inAt })}>Mark exit</button>
                      </li>
                    );
                  })}
                </>
              )}
              {[...inProgress, ...rest].map((f) => {
                const sessions = sessionRows(f);
                const range = displayRange(f);
                const hours = fmtHours(totalHours(f));
                return (
                  <li key={f.employee_id} className="pill-row">
                    <div className="pill-row-stack">
                      <div className="pill-row-main">
                        <span className="name" title={f.employee_id}>{f.name}</span>
                        <span className="meta">
                          {statusChip(f.status)}
                          <span className="time">{range}</span>
                          <span className="hours">{hours}</span>
                          {f.status === "IN_PROGRESS" && cur === today && (
                            <button className="link" onClick={() => openModal({ kind: "exit", employee_id: f.employee_id, date: cur, in_time: openInTime(f) })}>Mark exit</button>
                          )}
                        </span>
                      </div>
                      {sessions.length > 0 && (
                        <ul className="session-list">
                          {sessions.map((session) => {
                            const inTime = session.in_ist ? timePart(session.in_ist) : "";
                            const outTime = session.out_ist ? timePart(session.out_ist) : "";
                            const source = session.in_source ?? session.out_source ?? session.source ?? null;
                            return (
                              <li key={`${session.in_ist ?? "open"}-${session.out_ist ?? "open"}-${source ?? "unknown"}`}>
                                <span>{inTime && outTime ? `${inTime} → ${outTime}` : inTime || "Open session"}{typeof session.duration_s === "number" ? ` (${fmtHours(session.duration_s)})` : ""}</span>
                                {source === "manual" && <span className="chip">manual</span>}
                              </li>
                            );
                          })}
                        </ul>
                      )}
                    </div>
                  </li>
                );
              })}
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
  const { down, version, today, openModal } = useApp();
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
                {typeof data.incomplete === "number" && ` · ${data.incomplete} incomplete`}
                {typeof data.total_hours_s === "number" && ` · total ${fmtHours(data.total_hours_s)}`}
                {data.first_marked_day && ` · first ${data.first_marked_day}, last ${data.last_marked_day}`}</span>
            </p>
            {data.days_truncated && <p className="note">Showing the latest 366 days. Narrow the range to see older days.</p>}
            {data.days.length === 0 && <p className="muted">No attendance recorded in this range.</p>}
            <ul className="rows">
              {data.days.map((d) => {
                const rowClass = d.status === "INCOMPLETE" ? "pill-row incomplete" : "pill-row";
                const showExit = d.status === "INCOMPLETE" || (d.status === "IN_PROGRESS" && d.day === today);
                return (
                  <li key={d.day} className={rowClass} title={d.note || undefined}>
                    <span className="name">{fmtDay(d.day)}</span>
                    <span className="meta">
                      {d.status === "INCOMPLETE" && <span className="chip bad">Incomplete</span>}
                      {d.status === "IN_PROGRESS" && <span className="chip warn">IN</span>}
                      {d.source === "manual" ? <span className="chip">manual</span> : <span className="time">{timePart(d.marked_at_ist)}</span>}
                      {typeof d.hours_s === "number" && <span className="hours">{fmtHours(d.hours_s)}</span>}
                      {showExit && <button className="link" onClick={() => openModal({ kind: "exit", employee_id: emp, date: d.day, in_time: d.in_ist ?? null })}>Mark exit</button>}
                      {d.distance_m != null && <span className="muted"> {Math.round(d.distance_m)} m</span>}
                    </span>
                  </li>
                );
              })}
            </ul>
          </>
        )}
      </div>
    </section>
  );
}
