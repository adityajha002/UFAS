# Attendance protocol (`attend.v1`, `attend.v2`, `history.v1`)

Shared by the app and the Go server. If either side changes, change this file in the same commit.

## Identity
- On enrolment the app creates an EC P-256 key in the Android Keystore (never leaves the device).
- `public_key` sent to `/register`: X.509 SubjectPublicKeyInfo DER, **standard base64**.
- Enrolment code: 20 lowercase hex chars (`randHex(10)`), single use, issued by the office.

## Signed strings (UTF-8, `|` separated, no trailing newline)
    attend.v1|challengeID|nonce|facultyID|latMicro|lonMicro|accuracyCm
    attend.v2|action|challengeID|nonce|facultyID|latMicro|lonMicro|accuracyCm
    history.v1|challengeID|nonce|facultyID

- `action` (v2 only): exactly `in` or `out`, lower case. The Kotlin module refuses to sign anything else.
- `latMicro`, `lonMicro`: degrees x 1e6, rounded, signed integers.
- `accuracyCm`: metres x 100, rounded.
- Signature: `SHA256withECDSA`, ASN.1 DER, **standard base64**.
- The app builds these strings inside the Kotlin module (`PayloadBuilder.kt`); JS only passes fields.
  `modules/attendance-security/payload.ts` builds the same strings for the software fallback (no native module) and for the Jest tests.
- Server: `attendMsg()`, `attendMsgV2()` (added in SRV-2) and `historyMsg()` in `crypto.go` must produce the same bytes.

### Golden vectors
    attendMsg("c1","n1","f1",28600000,77200000,1500)  = attend.v1|c1|n1|f1|28600000|77200000|1500
    historyMsg("c1","n1","f1")                        = history.v1|c1|n1|f1
    attendMsgV2("out","c0ffee","bm9uY2U","EMP001",28606827,77209000,1500)
                                                      = attend.v2|out|c0ffee|bm9uY2U|EMP001|28606827|77209000|1500
The v1 vectors were checked on 2026-11-10 by comparing `PayloadBuilder.kt` with `server/server/crypto.go` (both use the same format).
The v2 vector comes from `00_OVERVIEW.md` §5.1; the server side does not exist until SRV-2.
All are asserted in `PayloadBuilderTest.kt` and `modules/attendance-security/payload.test.ts`.

## Endpoints (POST, JSON, HTTPS only)
`src/api/client.ts` is the only place in the app that knows them.
The server rejects unknown fields (`bad_request`), so the app must send exactly these fields.
**The nonce is never sent**: the server rebuilds the signed string from the nonce it stored for the challenge.

| Path | Request | Response |
|---|---|---|
| `/register` | `code`, `public_key` | 201 `faculty_id`, `name` |
| `/challenge` | `faculty_id` | 201 `challenge_id`, `nonce`, `expires_in` |
| `/attendance` v1 | `faculty_id`, `challenge_id`, `lat_micro`, `lon_micro`, `accuracy_cm`, `signature` | 201 `day`, `distance_m` |
| `/attendance` v2 | the v1 fields + `action` (`in`/`out`), signed with `attend.v2` | 201 `action`, `day`, `at_ist`, `distance_m`, `is_in`, `today_worked_s` (provisional until SRV-2) |
| `/history` | `faculty_id`, `challenge_id`, `signature` | 200 `name`, `today`, `marked_today`, `window_open`, `days` (`YYYY-MM-DD`, campus time); v2 fields arrive with SRV-2 |

A request without `action` is v1 and behaves exactly as before. A v2 request to a server without SRV-2 fails with `bad_request`, so the app sends v2 only when `/history` (fetched this session) contains `is_in`; otherwise it keeps the v1 flow.

## Challenges
- 60 s TTL. One use per signed request: each `/history` call and each *successful or consumed* `/attendance` call uses one.
- `poor_accuracy` and `outside_area` do **not** consume the challenge, so the app retries with a new fix on the same challenge (max 2 and 1 retries).
- `challenge_expired` / `challenge_used` / `challenge_invalid`: the app fetches a new challenge (max 2).
- v2: OUT goes through the same window and geofence checks as IN, so it can fail with `closed`, `outside_area` or `poor_accuracy`.

## Error codes (`{"error": "<code>"}`)
`closed`, `outside_area`, `poor_accuracy`, `already_marked` (v1), `already_in` (v2, 409), `not_in` (v2, 409),
`challenge_expired`, `challenge_used`, `challenge_invalid`, `too_many_challenges`, `not_enrolled`, `code_invalid`,
`bad_signature`, `bad_request`. Anything else on a 5xx is shown as a server error.

## Not enforced by the server
- Mock locations: the app refuses a fix whose `mocked` flag is true, but the signed message does not carry it, so the server cannot check it.
- Hardware-backed keys: no attestation under this protocol, so the server cannot tell a Keystore key from a software key or an emulator.
