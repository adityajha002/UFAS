import { useState } from "react";
import { api } from "../api";
import { errText, useApp } from "../ctx";
import { diffDays, firstOfMonth, isYmd } from "../lib/dates";
import { saveBlob } from "../lib/download";

export function DownloadBar() {
  const { today, toast } = useApp();
  const [from, setFrom] = useState(firstOfMonth(today));
  const [to, setTo] = useState(today);
  const [loc, setLoc] = useState(false);
  const [busy, setBusy] = useState(false);
  const [serverErr, setServerErr] = useState("");

  let problem = "";
  if (!isYmd(from) || !isYmd(to)) problem = "Pick both dates.";
  else if (from > to) problem = "From must not be after To.";
  else if (diffDays(from, to) + 1 > 366) problem = "Pick at most 366 days.";

  async function get() {
    setBusy(true); setServerErr("");
    try {
      const { blob, filename } = await api.exportCsv(from, to, loc);
      saveBlob(blob, filename);
      toast(`Downloaded ${filename}`);
    } catch (e) { setServerErr(errText(e)); } finally { setBusy(false); }
  }
  const msg = problem || serverErr;
  return (
    <div className="dl-wrap">
      <div className="download-bar">
        <h2>Download attendance</h2>
        <label>from <input type="date" value={from} max={to || today} onChange={(e) => { setFrom(e.target.value); setServerErr(""); }} /></label>
        <label>to <input type="date" value={to} max={today} onChange={(e) => { setTo(e.target.value); setServerErr(""); }} /></label>
        <label className="check" title="Adds latitude and longitude of each phone mark. Leave off unless you need it."><input type="checkbox" checked={loc} onChange={(e) => setLoc(e.target.checked)} /> + location</label>
        <button className="btn dark" disabled={!!problem || busy} onClick={get}>{busy ? "…" : "Get CSV"}</button>
      </div>
      {msg && <p className="field-err dl-msg" role="alert">{msg}</p>}
    </div>
  );
}
