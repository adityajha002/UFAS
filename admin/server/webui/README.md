# USICT Attendance Server: admin UI

React + Vite + TypeScript. No UI kit, no state library, no CDN. Runtime dependencies: `react`, `react-dom`, `qrcode`.
The built files in `dist/` are embedded into the Go binary (`webui.go`, `//go:embed all:webui/dist`) and served by the
admin listener without a token; only `/admin/api/*` needs the bearer token.

## Run it

1. Enable the admin listener once on the server host:

       ./server admin-token            # prints the token and its sha256
       # config.yaml
       admin:
         listen: "127.0.0.1:9090"
         token_sha256: "<paste the hash>"

2. Develop (hot reload, `/admin/api` proxied to 127.0.0.1:9090):

       cd webui && npm install && npm run dev

3. Ship: `npm run build` (typechecks, then writes `dist/`), then `go build` in `server/`.
   Open `http://127.0.0.1:9090/` and sign in with the token. From another machine use an SSH tunnel or a TLS proxy
   (`ssh -L 9090:127.0.0.1:9090 host`); do not bind the admin listener to 0.0.0.0 over plain HTTP.

`npm test` runs the unit tests (dates, config patch builder, API client). `npm run typecheck` only typechecks.

## Where things are

| Design box | File | Routes |
|---|---|---|
| Today's attendance / one user's attendance | `src/panels/AttendancePanel.tsx` | `GET /attendance/day`, `GET /faculty/{id}/attendance` |
| Server status + commands, shutdown | `src/panels/StatusPanel.tsx`, `src/components/dialogs.tsx` | `GET /status`, `POST /faculty`, `/faculty/{id}/code`, `/revoke`, `/attendance/manual`, `/server/shutdown` |
| Configurations | `src/panels/ConfigPanel.tsx`, `src/lib/config.ts` | `GET/PATCH /config`, `DELETE /config/overrides`, `GET /config/history` |
| Download attendance | `src/panels/DownloadBar.tsx` | `GET /export/attendance.csv` |
| Audit (phone requests, admin actions) | `src/panels/AuditPanel.tsx` | `GET /audit`, `GET /actions` |
| Users | `src/panels/UsersPanel.tsx` | `GET /faculty` |
| Login, DOWN detection, polling | `src/Login.tsx`, `src/App.tsx`, `src/hooks/usePolling.ts` | |

## Behaviour worth knowing

- "Today" and every date come from `status.server_time_ist` (IST), never from the browser clock.
- Token lives in `sessionStorage` only (cleared when the tab closes) and is sent only as an `Authorization` header.
- Two failed `/status` polls in a row show DOWN and pause everything else; the first success refetches all panels.
- Configuration changes are saved as database overrides. `config.yaml` itself is not rewritten (the "overridden" badge shows this).
- The enrollment code exists only in the open dialog; closing it discards it.
- Strict CSP (`default-src 'self'`): no inline scripts or style attributes, no external fonts.
- The app deliberately does not use React `StrictMode`, because the enrollment dialog issues a code when it opens.
