# Progress

Created 2026-11-10 during APP-0 (no `PROGRESS.md` existed in this folder). If you have the original one, merge this into it.

## App (`APP_PLAN.md`)
- [x] **APP-0** Discovery (2026-11-10): `app/IN_OUT_DISCOVERY.md`; later phases of `APP_PLAN.md` now name real paths. No source files changed. Tests not run: `app` has no `node_modules`.
- [ ] **APP-1** Signing v2 + API client — code and unit tests done 2026-11-10; **open:** Kotlin test not run, smoke test against SRV-2 (see below)
- [ ] **APP-2** State and button — code, unit tests and a web preview against a fake server done 2026-11-10; **open:** manual test on a phone against SRV-2 (script below)
- [ ] **APP-3** History colours + cache — code, unit tests and a web preview against a fake server done 2026-11-10; **open:** check on a phone against SRV-2, real `day_status` format and colours (00_OVERVIEW.md)
- [ ] APP-4 (deferred) Reminder

### APP-1 notes (2026-11-10)
Baseline before the APP-1 source changes (`npm ci`, then `npm test` and `npx tsc --noEmit` in `app`): 5 suites / 65 tests pass, type-check clean. `expo lint` not run (no ESLint config; the first run would add one).

After APP-1: **6 suites / 93 tests pass, `tsc --noEmit` clean.**

Changed (all under `app/`):
- Kotlin: `PayloadBuilder.attendV2(...)` (rejects any action other than `in`/`out`), `AttendanceSecurityModule` `signAttendanceV2` + `AttendanceV2FieldsRecord`, `PayloadBuilderTest` (v2 golden vector, `in` case, bad actions). v1 `attend`/`signAttendance` unchanged.
- JS module: new `modules/attendance-security/payload.ts` (pure string builders, used by the software fallback) + `payload.test.ts` (golden vectors); `index.ts` gains `signAttendanceV2`.
- `src/api/client.ts`: `attendanceV2()` sends `action`; `AttendanceV2Response` type (provisional until §5.1 / SRV-2). `src/api/errors.ts`: `already_in`, `not_in`.
- `src/lib/markAttendance.ts`: retry loop shared; new `markAttendanceV2(facultyId, action, deps)` (resolves `already_in`/`not_in`); v1 `markAttendance` behaves exactly as before (its tests unchanged and passing).
- `docs/PROTOCOL.md`: v2 string, vectors, no nonce in request bodies, new error codes.
- No screen or hook changed: the app still sends v1 until APP-2.

Not verified yet:
- `PayloadBuilderTest.kt` has not been compiled or run (no `android/` project or Kotlin compiler on this PC). Run it after a prebuild (below).
- Smoke test IN → OUT against a local server needs SRV-2 (not built; Go is not installed here).

**Native rebuild (required for APP-1's Kotlin change)** — Expo Go or an OTA update will not carry it:
1. Install JDK 17 and set `JAVA_HOME` to it (this PC has JDK 25; the Gradle 8.13 in `build.log` cannot run on it). Set `ANDROID_HOME` to `%LOCALAPPDATA%\Android\Sdk`.
2. `cd app && npx expo prebuild --platform android` (generates `android/`; or `--clean` to regenerate).
3. Kotlin unit test: `cd android && ./gradlew :attendance-security:testDebugUnitTest`.
4. Dev build on a phone: `npx expo run:android`. Release APK: `cd android && ./gradlew assembleRelease`.

### APP-2 notes (2026-11-10)
After APP-2: **7 suites / 124 tests pass, `tsc --noEmit` clean.**

What the card does now (`src/screens/HomeScreen.tsx`):
- Server reports `is_in` (SRV-2): button **MARK IN** / **MARK OUT**, sends that action. Instruction line 1: "PLEASE CLICK THE BUTTON BELOW TO RECORD YOUR ENTRY OR EXIT". "IN since hh:mm" under the button while IN.
- Success: "Marked IN at hh:mm" / "Marked OUT — worked 3h 20m today"; the button flips at once from the reply, then `/history` is re-fetched.
- `already_in` / `not_in`: message, then `/history` re-fetched (button corrects itself), no retry.
- `closed` / `outside_area` / `poor_accuracy` / network: existing message; IN/OUT state unchanged.
- Not known yet (no `/history` from the server this session; a cached `is_in` never decides): spinner while loading, otherwise grey disabled "MARK IN / OUT" with an offline or error message.
- **Deviation from the plan, accepted by the owner 2026-11-10 (keep it):** if the server's `/history` has no `is_in` (server without SRV-2), the card keeps the original v1 behaviour unchanged (MARK ATTENDANCE / MARKED, `attend.v1`). So the new app also works against today's server, and the deployment order no longer matters for the app.

Files: new `src/lib/inOut.ts` (+ `inOut.test.ts`), new `src/hooks/useMarkInOut.ts`; changed `src/hooks/useHistory.ts` (`afterMark`), `src/components/GreetingCard.tsx`, `src/components/MarkAttendanceButton.tsx` (`label` prop), `src/lib/markState.ts` (`unknown`), `src/api/client.ts` (optional `is_in`, `open_since_ist`, `today_worked_s` on `HistoryResponse`, provisional), `src/api/errors.ts` (`messageFor`), `docs/E2E-CHECKLIST.md`. `useMarkAttendance` (v1) unchanged.

Web preview check (Expo web + a fake API; software key and stubbed GPS, so not a test of the phone's Keystore or GPS):
IN → "Marked IN at 07:58", MARK OUT, "IN since 07:58" · OUT → "Marked OUT — worked 6h 20m today", MARK IN ·
server flipped behind the app → "You're already marked IN.", button corrected to MARK OUT · old-server shape → original v1 card ·
server down on launch → grey "MARK IN / OUT" + offline message.

**Manual test on a phone (APP-2 acceptance; needs SRV-2 + a new APK):**
1. Open the app: button MARK IN.
2. Tap MARK IN on campus: "Marked IN at …", button MARK OUT, "IN since …".
3. Tap MARK OUT: "Marked OUT — worked …", button MARK IN.
4. Tap MARK IN again (second session), later MARK OUT: worked time is the sum of both sessions.
5. Airplane mode, tap the button: network error shown, label unchanged. Restart the app in airplane mode: grey "MARK IN / OUT", offline message.
6. Outside the geofence, tap MARK OUT: `outside_area` message, still MARK OUT.

### APP-3 notes (2026-11-10)
After APP-3: **8 suites / 155 tests pass, `tsc --noEmit` clean.**

What the history does now:
- Day chips: **green "✓" COMPLETE**, **amber "•" IN_PROGRESS** (only on the server's `today`; an older IN_PROGRESS is shown red), **red "!" INCOMPLETE**, grey no record. Weekends still hidden, no special "today" style (unchanged).
- A date in `days` with no `day_status` entry (old server, days before the update) is green with no hours.
- Tap a chip: a line under the chips, e.g. "MONDAY 9/11 · COMPLETE · 6h 05m" (hours only when the server gives `worked_s`); tap again to hide. Screen readers get the same ("Monday, complete, worked 6h 05m").
- Legend under "PREVIOUSLY MARKED ATTENDANCE", shown only when the server sends per-day statuses.
- Cache: stored as `{ v: 2, data, savedAt }` under `history_cache:<facultyId>`. An entry written by the old app (no `v`) is read as days only, so it is green, never red, until the next `/history`. Unreadable or newer entries are ignored. Unused `history_cache` constant removed.
- Offline: cached colours shown; button disabled (from APP-2).

**Provisional (needs 00_OVERVIEW.md):**
- `day_status` shape: `parseDayStatus()` in `src/lib/dayStatus.ts` accepts `[{day, status, worked_s}]`, `{day: status}` and `{day: {status, worked_s}}`, and skips anything else. Once §5.2 is known, keep only the real form.
- Colours: `amber #FFB300`, `red #EF5350` in `src/theme/colors.ts` (chosen for contrast with the dark chip text; replace with §4 values).

Files: new `src/lib/dayStatus.ts` (+ `dayStatus.test.ts`), `src/components/StatusLegend.tsx`; changed `src/lib/groupByWeek.ts` (`DayInfo.status`, `workedS`), `src/lib/storage.ts`, `src/components/DayChip.tsx`, `src/components/WeekCard.tsx`, `src/screens/HomeScreen.tsx`, `src/theme/colors.ts`, `src/api/client.ts` (`today`, `day_status` on `HistoryResponse`), tests `src/lib/lib.test.ts`, `src/lib/history.test.ts`, `docs/E2E-CHECKLIST.md`.

Web preview check (fake API): this week Mon green, today amber after MARK IN; last week Mon/Tue/Thu green, Wed red; legend shown; tapping Monday shows "MONDAY 9/11 · COMPLETE · 6h 05m". Old-format cache with the server down: the INCOMPLETE day showed green, no legend, no crash.

### Layout fix (2026-11-10, asked by the owner after APP-3)
Problem: every size was scaled once, at start-up, from the 1080-px mockup to the window width, so text came out tiny on phones (body text about 8 dp) and a resized or wide browser window stretched the greeting card while the week cards stayed small.

Now (same colours, fonts and shapes):
- `src/lib/scale.ts`: sizes follow the window (`makeStyles`, `useLayout`); content is capped at 480 dp and centred (`src/app/index.tsx`), so tablets and browsers show a phone-width column.
- Larger text everywhere (body about 12–13 dp at 390 dp wide), full-width MARK IN / MARK OUT button, week cards and day boxes as wide as the greeting card, shared side margin.
- From common attendance-app patterns: a "Today:" line in the card ("Today: IN since 09:12 · worked 3h 20m" / "Today: OUT · worked 6h 20m" / "Today: not marked yet"; replaces the APP-2 "IN since" line under the button), today's day box outlined in yellow, and a month summary under the section title ("NOVEMBER · 5 COMPLETE · 1 INCOMPLETE"). Legend laid out in two columns.
- Not changed: `EnrollmentScreen` still uses the start-up scale (now capped at 480 dp, so it is no longer stretched in a browser).

Files: `src/lib/scale.ts` (+ `scale.test.ts`), `src/app/index.tsx`, `src/screens/HomeScreen.tsx`, `src/components/{Header,GreetingCard,MarkAttendanceButton,WeekCard,DayChip,StatusLegend,AttendanceHistory}.tsx`, `src/lib/groupByWeek.ts` (`isToday`, `monthSummary`), `src/lib/inOut.ts` (`todayLine`), tests `lib.test.ts`, `inOut.test.ts`, `docs/E2E-CHECKLIST.md`.
Result: **9 suites / 162 tests pass, `tsc --noEmit` clean.** Web preview checked at 375 px (phone) and 1280 px (centred column, re-laid out on resize).

Test tool: `app/scripts/mock-api.js` is a fake server for trying the screens (`node scripts/mock-api.js`; `/qr` shows an enrolment QR). It does not check signatures, location or time.

### Android emulator run (2026-11-10)
Built and installed a debug build on the Android Studio emulator (Medium Phone, API 37) and checked it against the fake server:
enrolment with a typed code (Keystore key created; StrongBox is not available on the emulator, so `KeyManager` fell back to the normal Keystore as designed),
home screen with the new layout, MARK IN with the emulator's GPS → request had `action: "in"` and a DER signature from the Kotlin module → button flipped to MARK OUT, today amber, "Today: IN since 10:44".
Not checked here: the real Go server (needs SRV-2), QR scanning with the camera, release build.

How to run it again (Windows, from `app`):
1. Java: use **JDK 17** (`C:\Program Files\Eclipse Adoptium\jdk-17.0.21.9-hotspot`, installed with winget). Java 25 (system default and Android Studio's JBR) fails the native build. Note: an old Oracle JDK 17 sits at the root of `E:\` and is broken (`jlink` fails); make sure `JAVA_HOME` points at the Temurin 17 folder.
2. Start the emulator from Android Studio (Device Manager), the fake server (`node scripts/mock-api.js`) and Metro (`npx expo start`, with `EXPO_PUBLIC_API_BASE_URL=http://localhost:8099`).
3. `adb reverse tcp:8081 tcp:8081` and `adb reverse tcp:8099 tcp:8099` (the emulator then reaches this PC's Metro and fake server as `localhost`), and set a location: `adb emu geo fix 77.0244 28.6068`.
4. First time / after native changes: `set JAVA_HOME=…jdk-17…` then `npx expo run:android --no-bundler` (about 10–15 min the first time; it generates `android/`). If a C++ step fails with "cannot open file … unspecified system_category error", run it again (Windows file locking).
5. If the app stays on a black or blue screen, restart Metro: after a native build it can stop serving bundles.

## Server (`SERVER_PLAN.md`)
- [ ] SRV-0 Baseline and sync guard — `server/` part done 2026-11-10; **waiting for `admin/server/` and `00_OVERVIEW.md`**
  - Go 1.27.3 installed on this PC (winget, `C:\Program Files\Go`).
  - `server` baseline: `gofmt -l .` clean, `go vet ./...` clean, `go test ./...` **24 tests pass**.
  - `scripts/check_sync.sh` written (compares `crypto.go`, `config_test.go` between `server` and `admin/server`); not yet passing because `admin/server/` is missing. Adjust its `B=` path if the admin code sits one level deeper.
  - Note: SRV-3 says to start the sweep "next to `pruneLoop`", but `server` has no `pruneLoop`; check the admin tree.
- [ ] SRV-1 Schema v2
- [ ] SRV-2 IN/OUT core
- [ ] SRV-3 Midnight sweep
- [ ] SRV-4a Admin reads
- [ ] SRV-4b Admin writes
- [ ] SRV-5 Audit action

## Open items
- `00_OVERVIEW.md` (API details §5, colours §4) is missing from this folder; needed before APP-1's response types are final.
- The server rejects unknown JSON fields such as `action`. Since APP-2 the app only sends v2 when `/history` reports `is_in`, so it keeps working (v1) against a server without SRV-2.
