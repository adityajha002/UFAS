import { useState } from "react";
import { api, ApiError } from "../api";
import { errText, useApp } from "../ctx";
import { usePolling } from "../hooks/usePolling";
import { buildPatch, describeChanges } from "../lib/config";
import type { ConfigField } from "../types";

export function ConfigPanel() {
  const { down, toast, notify, openModal } = useApp();
  const [fields, setFields] = useState<ConfigField[] | null>(null);
  const [draft, setDraft] = useState<Record<string, string>>({});   // only rows the user edited
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [loadErr, setLoadErr] = useState("");
  const [busy, setBusy] = useState(false);
  const [preview, setPreview] = useState("");

  // Refetch on load and on window focus; edited rows are never overwritten.
  usePolling(async () => {
    try { setFields((await api.config()).fields); setLoadErr(""); }
    catch (e) { setLoadErr(errText(e)); }
  }, 600_000, !down);

  const dirtyKeys = Object.keys(draft);
  const { patch, errors: localErrors, empty } = fields ? buildPatch(fields, draft) : { patch: {}, errors: {}, empty: true };

  function edit(f: ConfigField, v: string) {
    setPreview(""); setErrors((e) => { const n = { ...e }; delete n[f.key]; return n; });
    setDraft((d) => {
      const n = { ...d };
      if (v === String(f.value)) delete n[f.key]; else n[f.key] = v;
      return n;
    });
  }
  function cancel() { setDraft({}); setErrors({}); setPreview(""); }

  async function submit(dry: boolean) {
    if (Object.keys(localErrors).length) { setErrors(localErrors); return; }
    setBusy(true); setErrors({});
    try {
      const body = await api.patchConfig(patch, dry);
      if (dry) { setPreview(describeChanges(body.changed) || "No change."); return; }
      setFields(body.fields); setDraft({}); setPreview("");
      toast(`Saved: ${describeChanges(body.changed)}. Applies to the next attendance request.`);
      notify();
    } catch (e) {
      if (e instanceof ApiError && e.fields) setErrors(e.fields); else toast(errText(e), "error");
    } finally { setBusy(false); }
  }
  async function reset(f: ConfigField) {
    try {
      setFields((await api.resetOverride(f.key)).fields);
      setDraft((d) => { const n = { ...d }; delete n[f.key]; return n; });
      toast(`${f.label} restored to config.yaml (${f.yaml_value})`); notify();
    } catch (e) {
      if (e instanceof ApiError && e.fields) setErrors(e.fields); else toast(errText(e), "error");
    }
  }

  return (
    <section className="panel top" aria-label="Configuration">
      <div className="panel-head">
        <h2>Configuration</h2>
        <button className="link" onClick={() => openModal({ kind: "history" })}>History</button>
      </div>
      <div className="panel-body cfg">
        {loadErr && <p className="field-err" role="alert">{loadErr}</p>}
        {!fields && !loadErr && <p className="muted">Loading…</p>}
        {fields?.map((f) => {
          const shown = draft[f.key] ?? String(f.value);
          const err = errors[f.key] ?? localErrors[f.key];
          const overridden = f.source === "override";
          const id = `cfg-${f.key}`;
          return (
            <div key={f.key} className={f.editable ? "cfg-row" : "cfg-row ro"}>
              <div className="cfg-label">
                <label htmlFor={id}>{f.label}</label>
                {f.editable && overridden && <span className="chip warn" title={`config.yaml says ${f.yaml_value}`}>overridden</span>}
                {!f.editable && <span className="chip" title="Change config.yaml and restart the server">restart to change</span>}
              </div>
              <div className="cfg-input">
                {f.editable ? (
                  <input id={id} value={shown} inputMode={f.type === "time" ? "numeric" : "decimal"} aria-invalid={!!err}
                    placeholder={f.type === "time" ? "HH:MM" : undefined} onChange={(e) => edit(f, e.target.value)} />
                ) : (
                  <input id={id} value={shown} readOnly aria-readonly="true" />
                )}
                {f.unit && f.type !== "time" && <span className="unit">{f.unit}</span>}
                {f.editable && overridden && (
                  <button className="icon-btn" onClick={() => reset(f)} aria-label={`Restore ${f.label} to config.yaml`} title={`Restore config.yaml value (${f.yaml_value})`}>↺</button>
                )}
              </div>
              {err && <p className="field-err" role="alert">{err}</p>}
            </div>
          );
        })}
        <p className="muted small">Saved values are kept in the database and win over config.yaml. The file itself is not changed.</p>
        {dirtyKeys.length > 0 && (
          <div className="sticky-foot">
            {preview && <p className="small">{preview}</p>}
            <div className="dialog-actions">
              <button className="btn ghost" onClick={cancel} disabled={busy}>Cancel</button>
              <button className="btn" onClick={() => submit(true)} disabled={busy || empty}>Preview</button>
              <button className="btn primary" onClick={() => submit(false)} disabled={busy || empty}>Save changes</button>
            </div>
          </div>
        )}
      </div>
    </section>
  );
}
