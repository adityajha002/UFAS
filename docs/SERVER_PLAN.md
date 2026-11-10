# SERVER plan (Go: `server/` and `admin/server/`)

Read `00_OVERVIEW.md` first (contract, data model, rule R1: **both trees**). One phase per session.
Paths below are relative to the repo root. "Both trees" = `server/` and `admin/server/`.

Phases: **SRV-0** baseline → **SRV-1** schema → **SRV-2** IN/OUT core → **SRV-3** midnight sweep →
**SRV-4a** admin reads → **SRV-4b** admin writes → **SRV-5** audit action → *SRV-6 deferred*.

---

## SRV-0 — Baseline and sync guard (no behaviour change)

**Goal:** know the starting state and make drift between the two trees detectable.
**Depends on:** nothing.

Steps
1. `cd admin/server/webui && npm ci && npm run build` (creates `dist/`, required by `go:embed`).
2. In each tree run `gofmt -l .`, `go vet ./...`, `go test ./...`. Record pass/fail counts in `PROGRESS.md`. If something already fails, record it; do not fix unrelated failures.
3. Create `scripts/check_sync.sh` (bash, `set -euo pipefail`): `diff -q` each file listed in an array between `server/` and `admin/server/`, print which differ, exit 1 on any difference or missing file. Initial list: `crypto.go`, `config_test.go`. Later phases append their new shared files (`sessions_schema.go`, `sessions.go`, `sweep.go` and their `_test.go`). Make it executable.
4. Note in `PROGRESS.md` any pre-existing differences you noticed between the trees (do not remove them).

Acceptance: both trees build and tests are recorded; `scripts/check_sync.sh` exits 0.
Out of scope: any functional change.

---

## SRV-1 — Schema v2 + migration (additive, deployable alone)

**Goal:** add the new tables/columns for both binaries safely, without changing behaviour.
**Depends on:** SRV-0.

Files
- NEW `sessions_schema.go` — **identical in both trees**; add to `check_sync.sh`.
- `db.go` (each tree) — call the migration from `openDB`, after `schema` (and after `migrate` in the admin tree); update `deleteFaculty`.
- NEW `sessions_schema_test.go` — identical in both trees.

Steps
1. In `sessions_schema.go` implement `migrateV2(db *sql.DB) error` creating exactly the objects in `00_OVERVIEW.md` §3 (`attendance` new columns, `attendance_session` + indexes, `audit_log.action`).
2. Use your **own** helper `addColumnIfMissing` (do not reuse the name `ensureColumn`, it already exists in the admin tree and would collide). It checks `pragma_table_info` and **closes the cursor before** `ALTER` (single connection).
3. **Concurrency safety:** `server/` and `admin/server/` may start simultaneously. Take a dedicated connection (`db.Conn`), run `BEGIN IMMEDIATE`, re-read `PRAGMA user_version` *inside* the lock, apply the migration only if it is `< 2`, set `PRAGMA user_version = 2`, `COMMIT`; roll back on error. The second process must find version 2 and do nothing. Keep `busy_timeout` behaviour.
4. Backfill: `UPDATE attendance SET first_in = marked_at WHERE first_in IS NULL`. Do **not** create session rows for old data; do not touch `status` (default `COMPLETE`) or `sessions` (default 0).
5. Replace the `attendance_report` view: `DROP VIEW IF EXISTS` then `CREATE VIEW` keeping the existing columns first and in the same order (`employee_id, name, day, marked_at_ist, distance_m, note`), then appending `status, first_in_ist, last_out_ist, hours, sessions`. Check the CLI `report` command still works (it may read this view).
6. `deleteFaculty`: also `DELETE FROM attendance_session WHERE faculty_id=?` inside the same transaction. Do this in both trees.
7. The `attendance_session` DDL lives in the migration, not in the `schema` const, so the existing `schema` strings stay untouched.

Tests (`sessions_schema_test.go`)
- Build a temp DB with the **old** DDL and a phone row + a manual row; open with `openDB`; assert columns exist, `first_in` backfilled, `user_version == 2`, view queryable, no session rows.
- Opening the same file twice is idempotent; two concurrent `openDB` calls on the same file both succeed.
- `deleteFaculty` removes that faculty's session rows.
- Existing handler tests still pass unchanged.

Acceptance: both trees' tests green; `check_sync.sh` 0; no handler behaviour changed.
**Handoff note to write:** tell the owner to stop both servers and back up `attendance.db` (+ `-wal`, `-shm`) before the first run of the migrated binaries.

---

## SRV-2 — IN/OUT core (both trees)

**Goal:** phone API supports `action` in/out per `00_OVERVIEW.md` §5, plus the new `/history` fields.
**Depends on:** SRV-1.

Files
- `crypto.go` (identical): add `attendMsgV2(action, challengeID, nonce, facultyID string, latMicro, lonMicro, accuracyCm int64) string` producing exactly `attend.v2|…`. Keep `attendMsg` (v1).
- NEW `sessions.go` (identical): `recordIn`, `recordOut`, `recomputeDay`, `effStatusExpr` (shared SQL fragment implementing the effective-status rule), small result structs. They take a `*sql.Tx`, ids, `now`, `today`, geo values; no HTTP, no config. Do not reference identifiers that exist in only one tree (e.g. admin-only error values): return plain sentinel errors defined in this file.
- `handlers.go` (each tree, edited separately): `attendance` handler gets `Action string \`json:"action"\``. Branch: `""` → existing v1 code, **unchanged**; `"in"`/`"out"` → v2; else `bad_request`. v2 verifies `attendMsgV2`, then runs the same policy checks in the same order, then one transaction using `recordIn`/`recordOut`. Map sentinels to `already_in` / `not_in`. A failure inside the transaction must roll back and leave the challenge unused (existing pattern).
- Add `already_in`, `not_in` to the error table (HTTP 409, copy `already_marked`'s style).
- `history` handler (each tree): compute the new fields (`is_in`, `open_since_ist`, `today_status`, `today_worked_s`, `day_status`) inside the existing transaction; keep `days` / `marked_today` exactly as before. Use `effStatusExpr`. Leave `reminder_time` as `null`.
- Append `sessions.go`, `sessions_test.go` to `check_sync.sh`.

Tests (`sessions_test.go`, identical; reuse existing helpers from `server_test.go` for challenges/signing — read them first; fake clock via `s.now`)
- IN then OUT → one closed session, hours = difference, day `COMPLETE`.
- Two sessions in one day → hours summed; status `COMPLETE` only when none open.
- IN twice → `already_in`, challenge still reusable. OUT without IN → `not_in`.
- OUT outside geofence → `outside_area`; OUT with window closed → `closed`; poor accuracy → `poor_accuracy`.
- v2 request signed with the v1 string → `bad_signature`; unknown `action` → `bad_request`.
- **Legacy**: request without `action` behaves exactly as before (incl. `already_marked`); a v2 IN on a day that already has a legacy row works and `worked_s` stays NULL until a session closes.
- Clock moved past midnight with an open session: `history` reports that day `INCOMPLETE` (effective), and a fresh IN today succeeds.
- `history` field shapes match §5.2.
- Golden vector test for `attendMsgV2` (§5.1).

Acceptance: all tests green in both trees; `check_sync.sh` 0; old app flow (v1) verified by test.

---

## SRV-3 — Midnight sweep (both trees)

**Goal:** persist the INCOMPLETE flag after midnight IST, including after downtime.
**Depends on:** SRV-2.

Files: NEW `sweep.go` + `sweep_test.go` (identical, add to `check_sync.sh`); `main.go` `serve()` in both trees.

Steps
1. `func (s *Server) sweepIncomplete(ctx context.Context, now time.Time) (int64, error)` — one `UPDATE attendance SET status='INCOMPLETE' WHERE status='IN_PROGRESS' AND day < ?` with `today = ymd(todayIST(now))`. Return rows affected. Idempotent, short.
2. `func (s *Server) sweepLoop(ctx context.Context)` — run once immediately at startup (catches downtime), then loop: compute the next 00:00:05 IST from `s.now()` (do **not** use a fixed 24h ticker; recompute each wake) and wait for it or `ctx.Done()`. Log `sweep: flagged N day(s) incomplete` only when N > 0.
3. Start it in `serve()` next to `pruneLoop`, same cancellable context, in both trees. Both processes will run it; that is safe because it is idempotent.

Tests: with an injected `now`, rows `IN_PROGRESS` for yesterday become `INCOMPLETE`, today's stay; running twice changes nothing; a COMPLETE day is never touched; next-midnight computation correct around IST boundaries (23:59:59 → 00:00:05 next day).

Acceptance: tests green; the loop stops cleanly on shutdown (no goroutine left blocking `serve` exit).

---

## SRV-4a — Admin read endpoints (`admin/server` only)

**Goal:** admin APIs expose status, sessions, hours and the incomplete list (§6.1, §6.3).
**Depends on:** SRV-2 (needs `effStatusExpr`).

Files: `admin_day.go`, `admin_faculty.go`, `admin_status.go`, `admin_export.go`, `admin.go` (route), tests in `admin_test.go` style (add a new `admin_inout_test.go`).

Steps
1. `queryDay`: add the new fields. Fetch the day's sessions with a **second query after closing the first cursor** and attach them in Go. Keep all existing JSON fields/semantics. Add `in_progress` and `incomplete` to `summary`.
2. NEW `adminIncomplete` + route `GET /admin/api/attendance/incomplete` (use the existing range-validation helpers; default all time; cap 1000 newest first; `truncated`). Join `faculty` for `employee_id`/`name`; `session_id` and `in_ist` from the open session (day row without a session row cannot be INCOMPLETE).
3. `adminFacultyAttendance`/`queryFacultyDays` and the faculty list: new fields per §6.1.
4. `adminStatus`: `in_now`, `incomplete_open`.
5. `adminExport`: new default columns and `detail=sessions` mode per §6.3. Reuse `csvSafe`. Read everything first, release the connection, then write (existing D6 pattern).

Tests: day view for a mix (complete, in-progress, incomplete, legacy, absent); incomplete list incl. range and cap; legacy rows appear with `worked_s: null` and never in the incomplete list; CSV columns and `hours` rounding; unauthorised access still 401.

Acceptance: `go test ./...` green in `admin/server`; JSON matches §6.1 exactly.

---

## SRV-4b — Admin write endpoints (`admin/server` only)

**Goal:** admin can close an open session with an exit time, and mark a whole day with in/out times (§6.2).
**Depends on:** SRV-4a.

Files: NEW `admin_exit.go`; `admin_write.go` (`adminManualMark`), `db.go` (`markManual`), `admin.go` (route), `main.go` (CLI `mark`), admin error table in `handlers.go`, tests (`admin_write_test.go` style).

Steps
1. `closeSessionManual(db, employeeID, day, exitHM, note, now)` in `admin_exit.go`: one transaction; locate the faculty's open session for `day`; validate per P5 using IST wall-clock → unix; set `out_at`, `out_source='manual'`, `duration_s`, append the note; call `recomputeDay` (from `sessions.go`). Errors: not found employee → existing `not_found` helper; no open session → `no_open_session`; time rules → `bad_exit_time` (add codes `no_open_session` 409 and `bad_exit_time` 422 to the admin error table).
2. Handler `adminExit` + route `POST /admin/api/attendance/exit`; use `trackAction(r, "manual_exit", …)`; never log secrets. Update the comment listing action names in the admin `schema` (`admin_actions`).
3. Change `markManual` to `markManual(db, employeeID, day, inHM, outHM, note, now)`: creates one session (`in_source=out_source='manual'`, geo NULL) and the day row (`COMPLETE`, `sessions=1`, `worked_s`, `first_in`, `last_out`, `marked_at=in_at`, geo columns NULL so legacy "manual" detection still works). Existing day row → `alreadyMarkedError` as today.
4. `adminManualMark`: add required `in_time`, `out_time` (validate `HH:MM`, `in < out`, not future when today).
5. CLI `mark` in `admin/server/main.go`: `mark EMP DATE IN OUT NOTE`; update usage text. **Leave `server/`'s CLI/`markManual` as is** (legacy, no hours) and mention this in the handoff.

Tests: exit success (status → COMPLETE, hours correct, history reflects it); exit twice → `no_open_session`; exit time ≤ IN time / next day / future → `bad_exit_time`; admin_actions row written; manual mark with times; manual mark on existing day → `already_marked`; both flows work for yesterday's INCOMPLETE and for today's IN_PROGRESS.

Acceptance: tests green; manual-mark UI contract (§6.2) satisfied.

---

## SRV-5 — Audit records the action (both trees)

**Goal:** audit rows say whether an attempt was IN or OUT.
**Depends on:** SRV-1 (column exists), SRV-2.

Steps
1. Add `Action string` to `auditRec`; set it in the v2 `attendance` handler; `writeAudit` stores it in `audit_log.action` (NULL for other endpoints and v1 requests). Both trees' `writeAudit` differ — edit each.
2. Admin tree: `adminAuditList`/`adminAuditOne` return `action` (nullable). Add `action` to the admin audit filters only if a filter pattern already exists; otherwise just return it.
Tests: IN and OUT attempts (success and rejected) store the right action; v1 stores NULL.
Acceptance: green in both trees.

---

## SRV-6 — DEFERRED (only when the owner says go)

(a) **Reminder config:** new editable key `reminder.time` (`HH:MM`, default `17:00`, empty = disabled) following the existing config registry pattern (`settings.go` key list, validation, YAML default, admin Config panel field type `time`); expose it as `reminder_time` in `/history`. No server-side notification sending.
(b) **Remove v1 legacy `/attendance` path** after the owner confirms every phone runs the new app. Delete the `attend.v1` branch and its tests; keep `history.v1`.
