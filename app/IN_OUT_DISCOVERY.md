# APP-0 discovery: IN / OUT

Phase APP-0 of `APP_PLAN.md`. Read-only: no source file was changed.
Written 2026-11-10 from the code in this folder, checked against `server/*.go`.

**The app root is `app/`.** Paths below are relative to it unless they start with `server/`.

---

## 1. Project type and versions

| | |
|---|---|
| Type | Expo app, Continuous Native Generation (there is no `android/` or `ios/` folder; it is generated on build) |
| Expo SDK | 57 (`expo ~57.0.31`) |
| React Native / React | 0.86.3 / 19.2.3 |
| Router | `expo-router ~57.0.29`, one real route: `src/app/index.tsx` (`_layout.tsx` is a bare `<Slot />`) |
| TypeScript | ^5.6.3, `strict: true`, alias `@/*` → `src/*` |
| `app.json` | name "USICT", Android package `com.anonymous.USICT` (placeholder), experiments `typedRoutes` + `reactCompiler` |
| Storage | `@react-native-async-storage/async-storage 2.2.0` |
| Location / camera | `expo-location ~57.0.22`, `expo-camera ~57.0.6` (QR scan at enrolment) |

`package.json` scripts: `start` (expo start), `android` (expo run:android), `ios`, `web`, `lint` (expo lint), `test` (`jest --runInBand`), `reset-project` (**Expo template script, never run it: it moves the source away**).

- **Unit tests:** Jest 29 + ts-jest, `testEnvironment: node` (`jest.config.cjs`). Test files: `src/api/client.test.ts`, `src/lib/enroll.test.ts`, `src/lib/history.test.ts`, `src/lib/lib.test.ts`, `src/lib/markAttendance.test.ts`. Run with `npm test`.
- **Type-check:** `npx tsc --noEmit`.
- **Lint:** `npx expo lint`. There is no ESLint config yet, so the first run sets ESLint up and edits `package.json`.
- **Kotlin unit test:** `modules/attendance-security/android/src/test/.../PayloadBuilderTest.kt` (JUnit 4.13.2). It needs the generated `android/` project. Run it from `android/` with the module's Gradle `test…UnitTest` task.
- **Baseline not run:** this copy has no `node_modules`, so tests were not run in APP-0. APP-1 starts with `npm ci`, then records the test and type-check baseline in `PROGRESS.md`.
- This PC: Node v24.12.0, npm 11.6.2, OpenJDK 25. **Go is not installed.**
- Template leftovers that no attendance screen uses: `src/components/themed-text.tsx`, `themed-view.tsx`, `src/hooks/use-theme.ts`, `use-color-scheme*.ts`, `src/constants/theme.ts`, `src/global.css`, most of `assets/images/`.
- Stray files: `exit` is a saved coloured `git diff` of `package.json` (not code). `build.log` is a successful `./gradlew assembleRelease` from the Linux machine (`/home/aditya-jha/Documents/UFAS/app/android`, Gradle 8.13).

## 2. Native Kotlin module

**Location:** `modules/attendance-security/`. It is a local Expo module for Android only (`expo-module.config.json`), autolinked. The native module is named `AttendanceSecurity`.

| File | Contents |
|---|---|
| `android/src/main/java/com/usict/attendance/security/AttendanceSecurityModule.kt` | Exports async functions `hasKey`, `getKeyInfo`, `generateKey`, `signAttendance(AttendanceFieldsRecord)`, `signHistory(HistoryFieldsRecord)`, `deleteKey`. Record numbers arrive as `Double` and are turned into `Long` with `.toLong()`. |
| `…/PayloadBuilder.kt` | Pure Kotlin (no Android imports). **Builds the signed strings:** `attend(challengeId, nonce, facultyId, latMicro: Long, lonMicro: Long, accuracyCm: Long) = "attend.v1|$challengeId|$nonce|$facultyId|$latMicro|$lonMicro|$accuracyCm"` and `history(...) = "history.v1|$challengeId|$nonce|$facultyId"`. |
| `…/Signer.kt` | `sign(bytes)`: `SHA256withECDSA`, DER output, `Base64.NO_WRAP` (standard base64). Throws `IllegalStateException("key_missing")` when the key is gone. |
| `…/KeyManager.kt` | Alias `attendance_device_key`, EC `secp256r1`, `PURPOSE_SIGN`, `DIGEST_SHA256`. It tries StrongBox + unlocked-required, then TEE + unlocked-required, then TEE alone. `publicKeyDer()` returns SPKI DER, and there is a `securityLevel()` helper. |
| `android/src/test/…/PayloadBuilderTest.kt` | Asserts `attend.v1|c1|n1|f1|28600000|77200000|1500` and `history.v1|c1|n1|f1`. |
| `index.ts` | JS types (`AttendanceFields`, `HistoryFields`, `AttendanceSecurity`) plus `requireNativeModule('AttendanceSecurity')`. **If the native module is missing it silently falls back to a WebCrypto software key held in memory**, and builds the same strings in JS (`payload.attend`, `payload.history`). |

**How the key is used:** JS passes structured fields only. Kotlin builds the string, signs its UTF-8 bytes and returns `{ signature }`. The design comment says this stops a compromised JS layer from signing arbitrary text. v2 must keep that, so the `action` value is checked in Kotlin.

**Match with the server:** `attendMsg` and `historyMsg` in `server/crypto.go` use `fmt.Sprintf("attend.v1|%s|%s|%s|%d|%d|%d", …)`, which gives the same bytes as `PayloadBuilder`. The golden vectors that `docs/PROTOCOL.md` marks "UNVERIFIED" are correct, checked by reading both sides. Neither side was run.

**How JS calls it:** only through `src/native/attendanceSecurity.ts`, which re-exports the default and the types. Callers:
- `src/hooks/useMarkAttendance.ts` calls `native.signAttendance`.
- `src/hooks/useHistory.ts` calls `native.signHistory`.
- `src/app/index.tsx` calls `hasKey` and `deleteKey`.
- `src/screens/EnrollmentScreen.tsx` uses `generateKey` and `deleteKey` through `src/lib/enroll.ts`.

## 3. API client

`src/api/client.ts` is the only file that knows paths and field names.
- `createClient({ baseUrl, fetchImpl?, timeoutMs = 10_000 })` returns `register`, `challenge`, `attendance` and `history`, all as POST JSON.
- `api()` is a shared instance built from `process.env.EXPO_PUBLIC_API_BASE_URL`.
- `http://` is allowed in dev builds. Release builds need `https://` unless `EXPO_PUBLIC_ALLOW_HTTP=1`.

| Call | Body sent | Response type in app | What the server really returns (`server/handlers.go`) |
|---|---|---|---|
| `/register` | `code`, `public_key` | `{ faculty_id, name? }` | 201 `{faculty_id, name}` |
| `/challenge` | `faculty_id` | `{ challenge_id, nonce }` | 201 `{challenge_id, nonce, expires_in: 60}` |
| `/attendance` | `faculty_id, challenge_id, lat_micro, lon_micro, accuracy_cm, signature` | `{ ok?: boolean }` (unused) | 201 `{day, distance_m}` |
| `/history` | `faculty_id, challenge_id, signature` | `{ name, marked_today, window_open, days }` | 200, the same plus `today` |

- **The nonce is not sent.** The server rebuilds the signed string from the nonce it stored. `docs/PROTOCOL.md` wrongly lists `nonce` as a request field.
- **The server rejects unknown fields** (`decode()` uses `DisallowUnknownFields`, so the reply is `bad_request`). An app that sends `action` to today's server fails. See risk R2.

**Flow logic**
- `src/lib/markAttendance.ts` `markAttendance(facultyId, deps, onStage)`: fix → challenge → sign → POST.
  - `poor_accuracy`: new fix, same challenge, up to 2 retries.
  - `outside_area`: 1 retry.
  - `challenge_expired`, `challenge_used`, `challenge_invalid`: new challenge, up to 2 times. A challenge older than 55 s is also renewed before reuse.
  - `already_marked` resolves `{status:'already_marked'}`. Every other error is thrown.
- `src/lib/fetchHistory.ts` `fetchHistory`: challenge → `signHistory` → `/history`.

**Error mapping:** `src/api/errors.ts`, through `ApiError(code, status)` and `mapServerError(status, body)`.
- A known `{error}` code becomes that code.
- An unknown code becomes `server` if the status is 500 or higher, otherwise `unknown`.
- The server's `internal` error (500) therefore becomes `server`.

| Code | Origin | Message today |
|---|---|---|
| `closed` | server 409 | Attendance is closed right now. |
| `outside_area` | server 422 | You appear to be outside the campus area. |
| `poor_accuracy` | server 422 | Your location is not accurate enough. Move somewhere with a clearer sky and try again. |
| `already_marked` | server 409 | Your attendance is already marked for today. |
| `challenge_expired` | server 410 | The request took too long. Please try again. |
| `challenge_used` | server 409 | That request was already used. Please try again. |
| `challenge_invalid` | server 400 | That request was not recognised. Please try again. |
| `too_many_challenges` | server 429 | Too many requests. Wait a minute and try again. |
| `not_enrolled` | server 403 | This device is not enrolled. Please visit the office. |
| `code_invalid` | server 403 | That enrolment code is invalid, expired or already used. Ask the office for a new code. |
| `bad_signature` | server 401 | This device could not be verified. Re-enrol, or visit the office. |
| `bad_request` | server 400 | The app sent an invalid request. Please update the app or contact the office. |
| `network` / `timeout` | client | No connection… / The server took too long… |
| `server` / `bad_response` / `unknown` | client | The server had a problem… / Unexpected response… / Something went wrong… |

The client codes match the server's `errTable` exactly; there are no missing or extra codes.

**Text that mentions "already marked":**
- `errors.ts` `already_marked`.
- `src/hooks/useMarkAttendance.ts`: "Your attendance was already marked today" and `BASE_MESSAGE.marked` "Your attendance is marked for today".

**Location errors** are separate: `src/lib/fix.ts` `LocationError` (`permission_denied`, `permission_blocked`, `services_off`, `timeout`, `mocked`, `unavailable`) with `locationMessage()`. `src/lib/location.ts` `getFix()` takes one high-accuracy fix with a 15 s timeout.

## 4. Main screen

- **`src/app/index.tsx` `App`** has a boot state (`loading` / `enroll` / `ready`). It decides with `loadEnrollment()` and `native.hasKey()`, then renders `EnrollmentScreen` or `HomeScreen`. `reenrol()` deletes the key and wipes storage.
- **`src/screens/HomeScreen.tsx`** wires it all together:
  1. `useHistory(facultyId)`.
  2. `base = buttonBase(h.data, h.savedAt, now, h.fresh)`.
  3. `mark = useMarkAttendance({ facultyId, base, onMarked: h.refresh })`.
  4. `<GreetingCard state={mark.state} message={mark.message} onMark={mark.mark} />`.
  5. `weeks = groupByWeek(h.data?.days ?? [], Date.now())`.

  It also has full-screen "revoked" and "key_missing" views and an offline/error banner showing "Last updated …".
- **`src/components/GreetingCard.tsx`** shows `greeting()` from `src/lib/greeting.ts` (device clock: GOOD MORNING / AFTERNOON / EVENING) and three fixed instruction lines:
  - "PLEASE CLICK THE BUTTON BELOW TO UPLOAD THE ATTENDANCE"
  - "ATTENDANCE IS VERIFIED BY GEO-FENCING, PLEASE MAKE SURE YOU ARE IN {blockName}" (`blockName` defaults to `'E BLOCK'`)
  - "THIS PROCESS MAY TAKE A FEW SECONDS."

  Below them are an optional status `message` (yellow, live region) and the button.
- **`src/components/MarkAttendanceButton.tsx`** sets the label from `MarkState`:
  - `idle` / `closed`: "MARK ATTENDANCE"
  - `marked` / `success`: "MARKED"
  - `error`: "TRY AGAIN"
  - `working`: a spinner instead of a label

  It is enabled only for `idle`, `error` and `closed`.
- **State source:** plain React state in hooks; no store or context.
  - `MarkState` (`src/lib/markState.ts`) is `'idle' | 'closed' | 'marked' | 'working' | 'success' | 'error'`.
  - `buttonBase()` (`src/lib/historyView.ts`) gives `marked` from cached `marked_today` only if the cache was saved today (campus time). It gives `closed` only from fresh data. Everything else is `idle`.
  - `useMarkAttendance` holds a local `override` that is cleared whenever `base` changes. A `lock` ref blocks double taps. Stage messages are "Getting your location…" and "Verifying…".
- Today, after success, the button reads **MARKED and stays disabled for the rest of the day**. This whole-day lock has to go for IN/OUT.

## 5. Day indicators

- **Data:** `days: string[]` from `/history`. The server returns about the last two months of campus dates, newest first. The app shows the server data, or the cached copy (item 6).
- **`src/lib/groupByWeek.ts` `groupByWeek(days, nowMs, monthsBack = 2)`** returns `Week[]`, newest first. Each week has five `DayInfo { label, date, marked: boolean }` entries, Monday to Friday only (labels `MON TUES WEDS THU FRI`).
- **Weekends are never displayed**, and weekend dates in `days` are ignored. **Today gets no special treatment**: it is green if marked, otherwise grey, and later days of the current week are also grey.
- **Components:**
  - `src/components/AttendanceHistory.tsx`: a `FlatList` with pull-to-refresh.
  - `WeekCard.tsx`: two rows of chips (3 + 2) and a footer with `weekRange()` and `weekCaption()` from `src/lib/weekLabel.ts`.
  - **`DayChip.tsx`**: `marked` gives `colors.green` `#00C060` with a "✓ " prefix, otherwise `colors.chipGrey` `#DCDCDC`. Accessibility label is "Monday, marked" or "Monday, not marked".
- **Colours:** `src/theme/colors.ts` (values are approximate, taken from the mockup). There is **no amber or red token**. `orange #FF9A2E` is the card border colour, so amber should look different from it.
- **Dates:** `src/lib/dates.ts` uses a fixed IST offset. A "campus date" is read only with `getUTC*` methods.

## 6. Local history storage

`src/lib/storage.ts` uses AsyncStorage.

| Key | Value | Written | Read |
|---|---|---|---|
| `faculty_id`, `faculty_name` | strings | `saveEnrollment()` after `/register` | `loadEnrollment()` at boot |
| `history_cache:<facultyId>` | JSON `{ data: HistoryResponse, savedAt: number }`, **no version field** | `saveHistory()` after every successful `/history` (`useHistory.refresh`) | `loadHistory()` once when `HomeScreen` mounts, shown before the fetch finishes |
| `history_cache` | the constant `K.legacyHistory` is defined but never read or written | | |

- It is a **cache of `/history`**, not the source of truth. Fresh server data replaces it.
- When offline, the cache stays on screen with a banner.
- `clearAll()` calls `AsyncStorage.clear()`, which wipes everything. It runs on revoke, missing key and re-enrolment.
- For APP-3: old entries can be recognised because `data` has `days` but no new status field.

## 7. Notifications and permissions

- **No notification library is installed**: no `expo-notifications`, no other.
- `app.json` declares no Android permissions and lists only the `expo-router` and `expo-splash-screen` plugins. Location and camera permissions come from the `expo-location` and `expo-camera` library manifests merged at build time. Check the generated `android/app/src/main/AndroidManifest.xml` after `npx expo prebuild` if this matters.
- APP-4 would need `npx expo install expo-notifications`, `POST_NOTIFICATIONS` (Android 13+), exact-alarm handling and a native rebuild.

## 8. Build and run

**App, development build on an Android phone**
1. `cd app && npm ci`
2. Create `app/.env` containing:
   ```
   EXPO_PUBLIC_API_BASE_URL=http://<PC LAN IP>:8080
   ```
   Dev builds allow `http`.
3. `npx expo run:android`. This generates `android/`, builds a debug APK and installs it on a USB or Wi-Fi debugging phone or an emulator. It needs the Android SDK and a JDK that Gradle supports (see R6).
4. **Expo Go is not enough.** Without the native module the app silently uses the in-memory software key from `modules/attendance-security/index.ts`. That key is lost on every reload, so `/history` then fails with `bad_signature`.

**Release APK** (what `build.log` shows): from `app/android`, run `./gradlew assembleRelease`. Set `EXPO_PUBLIC_ALLOW_HTTP=1` if the server is plain `http`.

**Pointing the app at a local server**
- The server listens on `0.0.0.0:8080` (`server/config.yaml`).
- Phone and PC must be on the same Wi-Fi, and the Windows firewall must allow port 8080.
- `server/server` is a **Linux binary (ELF)** and won't run on Windows. Build it with Go (not installed here), or use WSL or the Linux machine.
- `db_file` in `config.yaml` is a Linux path. Change it for a local run.
- Test faculty: `server add EMP001 "Name"`, then `server code EMP001` prints a 20-hex enrolment code. Scan it or type it in the app.
- With the current config the window is open 24/7, including weekends (`windowOpenAt` returns early for 00:00–24:00), so `closed` can't be tested without changing it.

## 9. Edit list for APP-1, APP-2, APP-3

### APP-1: signing v2 and API client (no screen changes)
| File | Change |
|---|---|
| `modules/attendance-security/android/.../PayloadBuilder.kt` | Add `attendV2(action, challengeId, nonce, facultyId, latMicro, lonMicro, accuracyCm)` → `attend.v2|<action>|…`. Leave `attend` (v1) unchanged. |
| `…/AttendanceSecurityModule.kt` | New `AttendanceV2FieldsRecord` (fields + `action: String`) and `AsyncFunction("signAttendanceV2")`. **Reject any `action` other than `in`/`out` in Kotlin.** |
| `…/test/.../PayloadBuilderTest.kt` | Golden vector: `attendV2("out","c0ffee","bm9uY2U","EMP001",28606827,77209000,1500)` = `attend.v2|out|c0ffee|bm9uY2U|EMP001|28606827|77209000|1500`. |
| `modules/attendance-security/index.ts` | Types `AttendAction`, `AttendanceV2Fields`; `signAttendanceV2` in the interface; export a pure `attendV2Payload()` used by the fallback, so Jest can test the golden vector without Android. |
| `src/native/attendanceSecurity.ts` | Re-export the new types. |
| `src/api/client.ts` | `attendanceV2(a)` sends `action` in the body. New `AttendanceV2Response` (`action, day, at_ist, distance_m, is_in, today_worked_s`). Optional new `HistoryResponse` fields (needs §5.2, see R1). Keep `attendance()` (v1). |
| `src/api/errors.ts` | Add `already_in` ("You're already marked IN") and `not_in` ("You're not marked IN") to the code union, `MESSAGES` and `SERVER_CODES`. |
| `src/lib/markAttendance.ts` | Add `markAttendanceV2(facultyId, action, deps, onStage)` with the same retry rules. It resolves `{status:'already_in' \| 'not_in'}` instead of throwing. Keep `markAttendance` (v1), so `useMarkAttendance` and the screen stay untouched in APP-1. |
| Tests | `src/api/client.test.ts` (body shape with `action`, new codes); `src/lib/markAttendance.test.ts` (v2 flow and retries); new golden-vector Jest test for `attendV2Payload`. |

### APP-2: state and button
| File | Change |
|---|---|
| `src/lib/historyView.ts` | Replace `buttonBase()` with a function that returns `{ state, action: 'in' \| 'out' \| null }` from **fresh** `is_in`. Cached `is_in` must not decide the action (plan: the server is the truth). |
| `src/lib/markState.ts` | Remove the whole-day `marked` state (or stop producing it); keep `idle / closed / working / success / error`. |
| `src/hooks/useMarkAttendance.ts` | Take `action`, call `markAttendanceV2`. Success messages "Marked IN at …" / "Marked OUT — worked 3h 20m today" (from `at_ist` and `today_worked_s`). On `already_in`/`not_in`: show the message, then `onMarked()` refresh, no retry. Remove the `BASE_MESSAGE.marked` copy. |
| `src/components/MarkAttendanceButton.tsx` | Label "MARK IN" / "MARK OUT" by action, keep the style. Disabled while working or while the action is unknown. Remove "MARKED". |
| `src/components/GreetingCard.tsx` | Line 1 becomes "…TO RECORD YOUR ENTRY OR EXIT"; optional "IN since 09:12" line from `open_since_ist`. |
| `src/screens/HomeScreen.tsx` | Wire `action` from `useHistory` data into `useMarkAttendance`. |
| Tests | `src/lib/history.test.ts` (new button-state function), plus pure tests for the message formatting. |
| `docs/E2E-CHECKLIST.md` | Update the "MARKED" and "mark twice" steps. |

### APP-3: colours and cache
| File | Change |
|---|---|
| `src/lib/groupByWeek.ts` | `DayInfo.marked` becomes `status: 'COMPLETE' \| 'IN_PROGRESS' \| 'INCOMPLETE' \| 'NONE'` (+ `workedS`), built from `day_status`. Keep the Mon–Fri and weekend rules. |
| `src/components/DayChip.tsx`, `WeekCard.tsx` | Colour and accessibility label by status; hours where there is room. |
| `src/theme/colors.ts` | Add `amber` and `red` tokens, clearly different from `orange`. |
| `src/lib/storage.ts` | Versioned value `{ v: 2, data, savedAt }`. Read old entries as `days` → COMPLETE, **never red**. Remove the unused `legacyHistory` key. |
| `src/screens/HomeScreen.tsx` | Small legend under "PREVIOUSLY MARKED ATTENDANCE"; offline: button disabled with the existing offline message. |
| Tests | `src/lib/lib.test.ts` (status → colour, weeks); `src/lib/history.test.ts` (cache migration, no red from old cache). |

### Risks
- **R1. `00_OVERVIEW.md` is not in this folder.** APP-1's v2 string is known because APP_PLAN quotes it. But the exact `/history` and `/attendance` v2 field shapes (§5.2: `day_status` format, `open_since_ist`, `at_ist`, `today_worked_s`) and the §4 colours are not. They are needed before the APP-1 response types are final, and before APP-2 and APP-3. `PROGRESS.md` and `admin/server/` are also missing.
- **R2. Deployment order.** The server rejects unknown JSON fields, so a phone sending `action` gets `bad_request` from today's server. **SRV-2 must be deployed before the new app is installed**, and APP-1's smoke test needs an SRV-2 server.
- **R3. Native rebuild.** The new Kotlin function means a new APK. Expo Go or an over-the-air update won't carry it.
- **R4. Software fallback.** `modules/attendance-security/index.ts` silently swaps in an in-memory WebCrypto key when the native module is missing. v2 must be added there too, or that path crashes. Also consider failing loudly in release builds instead: a release build without the module would register a throw-away software key.
- **R5. The whole-day "marked" idea is spread across files:** `buttonBase`, `BASE_MESSAGE`, the button `LABEL`, `already_marked` handling and the E2E checklist. APP-2 must change all of them together.
- **R6. Toolchain on this PC.** `build.log` used Gradle 8.13, which most likely can't run on the installed JDK 25. Install JDK 17 (the React Native recommended JDK) and set `JAVA_HOME` before `npx expo run:android`. Go is not installed, and the server binary is Linux-only.
- **R7. `docs/PROTOCOL.md` is stale.** It lists `nonce` as a request field (sending it would be rejected) and marks the v1 vectors "UNVERIFIED" (they match). It also needs a v2 section in APP-1, because the file says both sides change together.
