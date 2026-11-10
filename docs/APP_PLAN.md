# APP plan (Expo / React Native, `app/`)

Read `00_OVERVIEW.md` first (phone API contract §5, golden vector, status colours §4).
**APP-0 is done: see `app/IN_OUT_DISCOVERY.md`.** The app root is **`app/`**; file paths in the phases below are relative to it.
Expo SDK 57 app (`app/.env` gets `EXPO_PUBLIC_API_BASE_URL=http://<ip>:8080`), device-bound ECDSA P-256 key, **a native Kotlin module (`modules/attendance-security/`) builds the strings that are signed**, the app keeps a **local history cache** (AsyncStorage), and it shows a card with a button labelled "MARK ATTENDANCE" and green/grey day indicators.
The server rejects unknown JSON fields (discovery R2); since APP-2 the app only sends `action` when `/history` reports `is_in` (SRV-2), and keeps the v1 flow against an older server. `00_OVERVIEW.md` is not in the repo folder yet (discovery R1).

Screen today (from the owner's screenshot): greeting ("GOOD EVENING"), instruction text about geo-fencing, a button "MARK ATTENDANCE". Target: the same card where the button reads **MARK IN** or **MARK OUT** depending on state.

Phases: **APP-0** discovery → **APP-1** signing + API → **APP-2** state and button → **APP-3** history colours + cache → *APP-4 deferred (reminder)*.

---

## APP-0 — Discovery (read-only; no code changes)

**Goal:** map the app so the next phases are exact.
**Depends on:** the owner providing the `app/` source in the session.

Produce `app/IN_OUT_DISCOVERY.md` containing, with file paths and function names:
1. Project type and versions (Expo SDK, RN version, `package.json` scripts, how to run unit tests, lint, type-check).
2. **Native Kotlin module**: where it lives (`android/…` or an Expo module), its exported functions, **exactly how the attend string is built** and how the key is used; how it is called from JS.
3. API client: where `/register`, `/challenge`, `/attendance`, `/history` are called; request/response types; how **error codes** map to user messages (list every code handled today).
4. The main screen: the component with the card/button/label ("MARK", "ATTENDANCE", greeting, instruction text), its state source (React state, store, context), loading/disabled handling.
5. Day indicators: component(s) that render green/grey days, where their data comes from, how today and weekends are treated.
6. **Local history storage**: what is persisted (keys, shape, library, e.g. AsyncStorage/SQLite/MMKV), when it is written/read, whether it is the source for the UI or a cache of `/history`.
7. Notification libraries already installed (for APP-4 only), permissions in the manifest.
8. Build/run instructions for a dev build on Android, and how to point the app at a local server.
9. A concrete, file-level edit list for APP-1, APP-2, APP-3, and any risk you see (e.g. native rebuild needed).
Then **edit this file's later phases**: replace guesses with the real paths.

Acceptance: `IN_OUT_DISCOVERY.md` exists and covers items 1–9; no source files changed; tick APP-0 in `PROGRESS.md`.

---

## APP-1 — Signing v2 + API client

**Goal:** the app can send `action: "in" | "out"` with a correct v2 signature (contract §5.1).
**Depends on:** APP-0. Integration test needs SRV-2 running; unit work can start from the golden vector.

Files (from APP-0 §9): `modules/attendance-security/android/src/main/java/com/usict/attendance/security/PayloadBuilder.kt`, `AttendanceSecurityModule.kt`, `android/src/test/.../PayloadBuilderTest.kt`, `modules/attendance-security/index.ts` (incl. the WebCrypto fallback), `src/native/attendanceSecurity.ts`, `src/api/client.ts`, `src/api/errors.ts`, `src/lib/markAttendance.ts` (add `markAttendanceV2`, keep v1 so no screen changes), tests `src/api/client.test.ts`, `src/lib/markAttendance.test.ts`, `docs/PROTOCOL.md` (add v2). First step: `npm ci`, record test/type-check baseline.

Steps
1. Native module: add a function that builds and signs
   `attend.v2|<action>|<challengeID>|<nonce>|<facultyID>|<latMicro>|<lonMicro>|<accuracyCm>`.
   Keep the v1 function untouched (rollback path).
2. **Unit test with the golden vector** from `00_OVERVIEW.md` §5.1: `action=out challengeID=c0ffee nonce=bm9uY2U facultyID=EMP001 lat=28606827 lon=77209000 acc=1500` must produce `attend.v2|out|c0ffee|bm9uY2U|EMP001|28606827|77209000|1500`. If the module cannot be unit-tested in isolation, extract the string builder into a pure function that can.
3. JS API client: `markAttendance(action)` sends `action` in the body and uses the v2 signature; extend response types (`action`, `day`, `at_ist`, `distance_m`, `is_in`, `today_worked_s`).
4. Error mapping: add user messages for `already_in` ("You're already marked IN") and `not_in` ("You're not marked IN"). Existing codes (`closed`, `outside_area`, `poor_accuracy`, …) keep their messages; make sure the wording does not say "attendance already marked" for the new flow. Note OUT can fail with the same geofence/window codes.
5. Do not change any screen yet.

Acceptance: unit tests (golden vector + body shape) pass; a manual smoke test against a local server (SRV-2) can mark IN then OUT using a temporary debug call; native rebuild instructions recorded in `PROGRESS.md`.

---

## APP-2 — State and button

**Goal:** the card shows **MARK IN / MARK OUT** and sends the matching action.
**Depends on:** APP-1.

Files (from APP-0 §9): `src/lib/historyView.ts` (`buttonBase` → state + action), `src/lib/markState.ts` (drop whole-day `marked`), `src/hooks/useMarkAttendance.ts`, `src/components/MarkAttendanceButton.tsx` (`LABEL`), `src/components/GreetingCard.tsx` (copy), `src/screens/HomeScreen.tsx`, tests `src/lib/history.test.ts`, `docs/E2E-CHECKLIST.md`.

Steps
1. Read `is_in` (and `today_worked_s`, `open_since_ist`) from `/history` into the app state used by the main screen. The server is the source of truth; never infer IN/OUT from local history alone.
2. Button label: `MARK IN` when `is_in === false`, `MARK OUT` when `true` (match the existing visual style; the owner's design replaces the word "ATTENDANCE" with IN/OUT). Send `action` accordingly.
3. After a successful call: refresh `/history` and update state from the server response (`is_in`); show a short success message ("Marked IN at …" / "Marked OUT — worked 3h 20m today").
4. On `already_in` / `not_in`: show the message, then **refresh state** (the UI was stale), do not retry automatically.
5. Window closed / outside area / poor accuracy while marking OUT: show the existing messages; state stays IN.
6. Update the instruction text minimally to cover both actions (e.g. "…to record your entry or exit"); keep the rest of the copy.
7. Disable the button while a request is in flight; guard against double taps.
8. Optional small line under the button when IN: "IN since 09:12" (formatted from `open_since_ist`).

Acceptance: manual test script in `PROGRESS.md` passes: IN → button flips to OUT → OUT → flips to IN → IN again (second session) → hours accumulate; airplane-mode/network failure leaves state unchanged and shows the existing error.

---

## APP-3 — History colours and local cache

**Goal:** green / amber / **red** / grey day indicators from `day_status`; local history still works.
**Depends on:** APP-2.

Files (from APP-0 §9): `src/lib/groupByWeek.ts` (`DayInfo.marked` → `status`), `src/components/DayChip.tsx`, `src/components/WeekCard.tsx`, `src/theme/colors.ts` (add amber, red), `src/lib/storage.ts` (cache key `history_cache:<facultyId>`, add a version), `src/screens/HomeScreen.tsx` (legend), tests `src/lib/lib.test.ts`, `src/lib/history.test.ts`. Weekends are not shown at all today; "today" has no special treatment.

Steps
1. Use `day_status` from `/history` (contract §5.2): `COMPLETE` green, `IN_PROGRESS` amber (today only), `INCOMPLETE` **red**, no record grey. Keep the existing weekend/today treatment.
2. Local history/cache: version the stored shape. Data cached by the old app has only day strings: treat those as **COMPLETE** (green) until the next successful `/history` refresh replaces it; **never show red from old cache**.
3. Offline behaviour: when `/history` fails, show the cached statuses as before; do not show a button state you cannot trust (disable the button with the existing offline message if that is what it does today).
4. Show hours where there is room (e.g. on tap or a subtitle: "6h 05m"); legacy days (`worked_s: null`) show no hours.
5. Add a tiny legend if the screen has space (green complete · amber in progress · red incomplete · grey none).
6. Tests for the status→colour mapping and cache-version migration.

Acceptance: with server data containing each status, the indicators render as specified; upgrading from an old cache does not crash or show red.

---

## APP-4 — DEFERRED: local reminder (experimental, owner will decide later)

Do **not** start without the owner's go-ahead (also needs SRV-6a for `reminder_time`).
Outline for later: schedule a local notification at `reminder_time` (default 17:00) when the user marks IN (and is still IN at that time); cancel it when OUT is marked; re-sync the time on every `/history`. Consider Android exact-alarm and battery-optimisation limits; the midnight INCOMPLETE flag is the safety net, so the reminder may be best-effort. No server push.
