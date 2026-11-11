import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import logoUrl from "./asset/logo.png";
import { api, ApiError, getServerUrl, getToken, setServerUrl, setToken, setUnauthorizedHandler } from "./api";
import { CodeModal, DeleteDialog, ExitRequestModal, HistoryModal, MarkModal, RevokeDialog, ShutdownDialog } from "./components/dialogs";
import { Toasts, type ToastItem } from "./components/Toasts";
import { Ctx, type AppCtx, type AuditPreset } from "./ctx";
import { usePolling } from "./hooks/usePolling";
import { AttendancePanel } from "./panels/AttendancePanel";
import { AuditPanel } from "./panels/AuditPanel";
import { ConfigPanel } from "./panels/ConfigPanel";
import { DownloadBar } from "./panels/DownloadBar";
import { StatusPanel } from "./panels/StatusPanel";
import { UsersPanel } from "./panels/UsersPanel";
import type { FacultyRow, Modal, Status } from "./types";

export default function App() {
  const [status, setStatus] = useState<Status | null>(null);
  const signOut = useCallback(() => { setStatus(null); }, []);
  useEffect(() => { setUnauthorizedHandler(() => signOut()); return () => setUnauthorizedHandler(null); }, [signOut]);
  return <Dashboard initial={status} signOut={() => signOut()} />;
}

function Dashboard({ initial, signOut }: { initial: Status | null; signOut: () => void }) {
  const [status, setStatus] = useState<Status | null>(initial);
  const [down, setDown] = useState(false);
  const [authError, setAuthError] = useState<string | null>(null);
  const [stopped, setStopped] = useState(false);
  const [urlDraft, setUrlDraft] = useState<string>(() => getServerUrl());
  const [tokenDraft, setTokenDraft] = useState<string>(() => getToken() ?? "");
  const [connTick, setConnTick] = useState(0);
  const fails = useRef(0);
  const downRef = useRef(false);
  const [users, setUsers] = useState<FacultyRow[]>([]);
  const [version, setVersion] = useState(0);
  const [modal, setModal] = useState<Modal | null>(null);
  const [toasts, setToasts] = useState<ToastItem[]>([]);
  const [selected, setSelected] = useState<string | null>(null);
  const [preset, setPreset] = useState<AuditPreset>(null);
  const [menuOpen, setMenuOpen] = useState(false);
  const toastId = useRef(0);

  // /status doubles as the DOWN detector: two network failures in a row.
  usePolling(async () => {
    try {
      const s = await api.status();
      fails.current = 0;
      setAuthError(null);
      setStatus(s);
      if (downRef.current) { // first success after DOWN: everything refetches
        downRef.current = false; setDown(false); setStopped(false); setVersion((v) => v + 1);
      }
    } catch (e) {
      if (e instanceof ApiError) {
        if (e.code === "unauthorized" || e.status === 401) {
          setAuthError("Admin token required or invalid.");
          setStatus(null);
        }
        return;
      }
      fails.current++;
      if (fails.current >= 2 && !downRef.current) { downRef.current = true; setDown(true); }
    }
  }, down ? 3_000 : 5_000, !authError, 30_000, [connTick]);

  const loadUsers = useCallback(async () => {
    try { setUsers((await api.faculty()).faculty); } catch { /* the status poll reports connectivity */ }
  }, []);
  usePolling(loadUsers, 60_000, !down, null, [version]);

  const toast = useCallback((msg: string, kind: "ok" | "error" = "ok") => {
    const id = ++toastId.current;
    setToasts((t) => [...t, { id, msg, kind }]);
    setTimeout(() => setToasts((t) => t.filter((x) => x.id !== id)), kind === "error" ? 8000 : 4500);
  }, []);

  // Leaving user mode if that user disappears is not possible (no delete), so no check needed.
  const ctx: AppCtx | null = useMemo(() => status && ({
    status, down, stopped, users, version,
    today: status.server_time_ist.slice(0, 10),
    notify: () => setVersion((v) => v + 1),
    toast, openModal: setModal, closeModal: () => setModal(null),
    markStopping: () => setStopped(true),
    auditPreset: preset,
    requestAuditFilter: (outcome: string) => setPreset({ outcome, nonce: Date.now() }),
  }), [status, down, stopped, users, version, toast, preset]);

  const applyConnectionSettings = useCallback((nextUrl: string, nextToken: string) => {
    setServerUrl(nextUrl.trim() ? nextUrl : null);
    setToken(nextToken.trim() ? nextToken.trim() : null);
    fails.current = 0;
    downRef.current = false;
    setDown(false);
    setAuthError(null);
    setStatus(null);
    setConnTick((t) => t + 1);
  }, []);

  if (!ctx) {
    return (
      <div className="login-wrap">
        <header className="topbar"><h1>USICT Attendance Server</h1></header>
        <div className="login">
          <p aria-live="polite">
            {authError
              ? authError
              : down
                ? "Cannot reach the server. Verify the server URL or tunnel below."
                : "Connecting to the server…"}
          </p>
          <form
            onSubmit={(e) => {
              e.preventDefault();
              applyConnectionSettings(urlDraft, tokenDraft);
            }}
            style={{ display: "grid", gap: "0.75rem", marginTop: "0.75rem", textAlign: "left" }}
          >
            <label style={{ display: "grid", gap: "0.25rem", fontSize: "0.875rem" }}>
              <span>Server / Cloudflare Tunnel URL (leave blank to use default proxy)</span>
              <input
                type="text"
                value={urlDraft}
                onChange={(e) => setUrlDraft(e.target.value)}
                placeholder="https://your-tunnel.trycloudflare.com"
              />
            </label>
            <label style={{ display: "grid", gap: "0.25rem", fontSize: "0.875rem" }}>
              <span>Admin Token (optional if disabled on server)</span>
              <input
                type="password"
                value={tokenDraft}
                onChange={(e) => setTokenDraft(e.target.value)}
                placeholder="Bearer token"
              />
            </label>
            <div style={{ display: "flex", gap: "0.5rem", justifyContent: "flex-end" }}>
              {urlDraft && (
                <button
                  type="button"
                  className="btn"
                  onClick={() => {
                    setUrlDraft("");
                    applyConnectionSettings("", tokenDraft);
                  }}
                >
                  Use Default Proxy
                </button>
              )}
              <button type="submit" className="btn primary">Connect</button>
            </div>
          </form>
        </div>
      </div>
    );
  }

  return (
    <Ctx.Provider value={ctx}>
      <div className="app">
        <header className="topbar">
          <h1>USICT Faculty Attendance Server</h1>
          <div className="account">
            <button
              className="avatar"
              aria-label="Account menu"
              aria-haspopup="menu"
              aria-expanded={menuOpen}
              onClick={() => setMenuOpen(!menuOpen)}
              style={{
                backgroundImage: `url(${logoUrl})`,
                backgroundSize: "cover",
                backgroundPosition: "center",
                backgroundRepeat: "no-repeat",
                backgroundClip: "padding-box",
              }}
            />
            {menuOpen && (
              <div className="menu right" role="menu">
                <button
                  role="menuitem"
                  onClick={() => {
                    setMenuOpen(false);
                    setStatus(null);
                    setAuthError("Update server connection settings:");
                  }}
                >
                  Server URL ({getServerUrl() || "proxy"})
                </button>
                <button
                  role="menuitem"
                  onClick={() => {
                    setMenuOpen(false);
                    setStatus(null);
                    signOut();
                  }}
                >
                  Sign out
                </button>
              </div>
            )}
          </div>
        </header>
        {down && (
          <div className="down-banner" role="alert">
            {stopped
              ? "Server stopped. Start it again on the host with “server serve”. This page reconnects by itself."
              : "Cannot reach the server. Retrying every few seconds."}
          </div>
        )}
        <main className="grid">
          <AttendancePanel selected={selected} onBack={() => setSelected(null)} />
          <StatusPanel />
          <ConfigPanel />
          <div className="left-col">
            <DownloadBar />
            <AuditPanel />
          </div>
          <UsersPanel selected={selected} onSelect={setSelected} />
        </main>
        {modal?.kind === "code" && <CodeModal employeeId={modal.employee_id} />}
        {modal?.kind === "revoke" && <RevokeDialog employeeId={modal.employee_id} />}
        {modal?.kind === "delete" && <DeleteDialog employeeId={modal.employee_id} />}
        {modal?.kind === "mark" && <MarkModal employeeId={modal.employee_id} date={modal.date} />}
        {modal?.kind === "shutdown" && <ShutdownDialog />}
        {modal?.kind === "history" && <HistoryModal />}
        {modal?.kind === "exit_request" && <ExitRequestModal requestId={modal.request_id} employeeId={modal.employee_id} name={modal.name} />}
        <Toasts items={toasts} />
      </div>
    </Ctx.Provider>
  );
}
