# Manual end-to-end checklist (real Android device, release build)

Server: HTTPS reachable, test faculty record, a fresh enrolment code per run, campus geofence known.

## Happy path (server with SRV-2: IN/OUT)
- [ ] Fresh install, no code: enrolment screen appears
- [ ] Scan the QR: Home shows the faculty name, history loads, button shows MARK IN
- [ ] MARK IN inside the area during the window: "Marked IN at hh:mm", button flips to MARK OUT, card shows "Today: IN since hh:mm", today appears in history (amber, yellow outline)
- [ ] MARK OUT: "Marked OUT — worked …", button flips to MARK IN
- [ ] MARK IN again the same day, then OUT: hours add up across both sessions
- [ ] Kill and reopen while IN: still enrolled, button shows MARK OUT once history has loaded (spinner until then)
- [ ] Old server (no SRV-2): card shows the original "UPLOAD THE ATTENDANCE" text and MARK ATTENDANCE / MARKED, as before

## Enrolment errors
- [ ] Used or expired code: "ask the office for a new code", no key left behind (re-scan with a new code works)
- [ ] Camera permission denied: message and Allow/Settings button; manual entry still works
- [ ] Manual entry: spaces/dashes accepted, upper case accepted, wrong length rejected
- [ ] Airplane mode during enrol: network error, then a clean retry

## History colours (server with SRV-2)
- [ ] Legend under "PREVIOUSLY MARKED ATTENDANCE": complete / in progress / incomplete / no record
- [ ] While IN today: today's chip amber ("• "); after OUT: green ("✓ ")
- [ ] A day left IN past midnight: red ("! ") the next day
- [ ] Days marked before the IN/OUT update: green, no hours
- [ ] Tap a chip: line under the chips, e.g. "MONDAY 9/11 · COMPLETE · 6h 05m"; tap again to hide
- [ ] Install the new app over the old one (old cache on the phone), airplane mode on: old days green, nothing red, no crash
- [ ] Old server (no SRV-2): no legend, marked days green as before

## Marking
- [ ] Stale state (e.g. admin closes the open session, then tap MARK OUT without refreshing): "You're not marked IN.", button flips to MARK IN, no automatic retry
- [ ] Old server only: mark twice: second shows already marked
- [ ] Outside the area (both IN and OUT): `outside_area` message, button usable again; after a failed OUT it still says MARK OUT
- [ ] Weak GPS (indoors): `poor_accuracy` retries, then a clear message
- [ ] Outside 09:00-18:00 or a weekend: closed
- [ ] Location permission denied once, then "don't ask again": Settings instructions
- [ ] Location services off: message
- [ ] Developer option "mock location app" on: refused with the mock-location message
- [ ] Double tap on the button: one request only

## State handling
- [ ] Revoke the faculty on the server: Home shows the "no longer enrolled" screen
- [ ] Airplane mode with cached history: banner, saved history, "last updated" shown; button grey "MARK IN / OUT", disabled, with the offline message
- [ ] Airplane mode while marking: network error shown, IN/OUT state unchanged
- [ ] Airplane mode on first launch with no cache: "offline, no saved history" and Try again
- [ ] Pull to refresh updates history
- [ ] Return from background after 15 s+: history refreshes
- [ ] Clear app data / restore to a new phone (stored id but no key): enrol screen, not a crash

## Layout
- [ ] Small phone (e.g. 360 dp wide) and large phone: text readable, three day boxes fit per row, nothing cut off
- [ ] Tablet or browser: phone-width column centred, not stretched

## Devices
- [ ] A phone with StrongBox and one without
- [ ] A phone with no screen lock set: enrol either works or shows the key error message
- [ ] Release build (ProGuard on): all of the above still works
