import { useState } from "react";
import { api, ApiError, NetworkError } from "./api";
import type { ServerStatus } from "./types";

export function Login({ onDone, notice }: { onDone: (s: ServerStatus) => void; notice?: string }) {
  const [token, setToken] = useState("");
  const [err, setErr] = useState(notice ?? "");
  const [busy, setBusy] = useState(false);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true); setErr("");
    try {
      onDone(await api.login(token.trim()));
    } catch (x) {
      if (x instanceof ApiError && x.code === "unauthorized") setErr("Wrong token. Check the token printed by server admin-token.");
      else if (x instanceof ApiError && x.status === 429) setErr(`Too many wrong attempts. Wait ${x.retryAfter ?? 60} seconds and try again.`);
      else if (x instanceof NetworkError) setErr("Cannot reach the server. Is it running?");
      else setErr(x instanceof Error ? x.message : "Sign-in failed");
    } finally { setBusy(false); }
  }
  return (
    <div className="login-wrap">
      <header className="topbar"><h1>USICT Attendance Server</h1></header>
      <form className="login" onSubmit={submit}>
        <h2>Admin sign-in</h2>
        <label htmlFor="tok">Admin token</label>
        <input id="tok" type="password" autoComplete="off" autoFocus value={token} onChange={(e) => setToken(e.target.value)} />
        {err && <p className="field-err" role="alert">{err}</p>}
        <button className="btn primary" disabled={busy || !token.trim()}>{busy ? "Checking…" : "Sign in"}</button>
        <p className="muted small">Get a token by running <code>server admin-token</code> on the server host.</p>
      </form>
    </div>
  );
}
