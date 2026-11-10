// FAKE attendance API for trying the app's screens without the Go server. Development only.
// It does NOT check signatures, location or the time window, and forgets everything on restart.
//
//   node scripts/mock-api.js            (port 8099, all network interfaces)
//
// Point the app at it with EXPO_PUBLIC_API_BASE_URL in app/export/.env:
//   Android emulator:  http://10.0.2.2:8099      (10.0.2.2 = this PC, seen from the emulator)
//   Browser on this PC: http://localhost:8099
//   Phone on the same Wi-Fi: http://<this PC's LAN IP>:8099
// Any 20-character code (0-9, a-f) enrols. GET /qr shows an enrolment code as a QR.
// POST /mock/v1 toggles the old /history shape (no IN/OUT); POST /mock/flip flips IN/OUT behind the app.
const http = require('http');
const PORT = Number(process.env.PORT || 8099);
let isIn = false, openSince = null, workedS = 4 * 3600 + 5 * 60, v1 = false, n = 0;

const istNow = () => {
  const d = new Date(Date.now() + 5.5 * 3600e3);
  return d.toISOString().replace('Z', '+05:30').replace(/\.\d+/, '');
};
const today = () => istNow().slice(0, 10);

function send(res, status, body) {
  res.writeHead(status, {
    'Content-Type': 'application/json',
    'Access-Control-Allow-Origin': '*',
    'Access-Control-Allow-Headers': 'Content-Type',
    'Access-Control-Allow-Methods': 'POST, OPTIONS',
  });
  res.end(body === undefined ? '' : JSON.stringify(body));
}

// GET /qr: shows an enrolment code as a QR (random, or type the one `server code EMP001` printed).
const QR_PAGE = `<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><title>Enrolment QR</title>
<style>
  body{font-family:system-ui,sans-serif;margin:0;padding:24px 16px;background:#F8F1DF;color:#0B0044;text-align:center}
  h1{font-size:22px;margin:0 0 8px} p{margin:6px 0}
  #qr{display:block;width:fit-content;padding:16px;background:#fff;border-radius:12px;margin:16px auto}
  input{font:20px ui-monospace,monospace;width:22ch;max-width:100%;padding:8px;border:2px solid #0B0044;border-radius:8px}
  button{font:600 16px system-ui;padding:10px 16px;margin:10px 4px;background:#FFE05A;color:#0B0044;border:0;border-radius:8px;cursor:pointer}
  #msg{font-family:ui-monospace,monospace}
</style>
<script src="https://cdnjs.cloudflare.com/ajax/libs/qrcodejs/1.0.0/qrcode.min.js"></script></head>
<body><h1>Enrolment code QR</h1>
<p>Point the app's camera (enrolment screen) at this code.</p>
<input id="code" maxlength="24" aria-label="Enrolment code" spellcheck="false"><br>
<button id="new">New random code</button>
<div id="qr"></div><p id="msg"></p>
<script>
  var box = document.getElementById('qr'), input = document.getElementById('code'), msg = document.getElementById('msg');
  var qr = new QRCode(box, { width: 280, height: 280, correctLevel: QRCode.CorrectLevel.M });
  function rand() { return Array.from(crypto.getRandomValues(new Uint8Array(10)), function (b) { return b.toString(16).padStart(2, '0'); }).join(''); }
  function draw() {
    var c = input.value.replace(/[\\s-]/g, '').toLowerCase();
    if (/^[0-9a-f]{20}$/.test(c)) { qr.makeCode(c); box.style.visibility = 'visible'; msg.textContent = 'Code: ' + c; }
    else { box.style.visibility = 'hidden'; msg.textContent = 'Enter 20 characters, 0-9 and a-f'; }
  }
  input.oninput = draw;
  document.getElementById('new').onclick = function () { input.value = rand(); draw(); };
  input.value = rand(); draw();
</script></body></html>`;

http.createServer((req, res) => {
  if (req.method === 'OPTIONS') return send(res, 204);
  if (req.method === 'GET' && req.url.split('?')[0] === '/qr') {
    res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' });
    return res.end(QR_PAGE);
  }
  let raw = '';
  req.on('data', (c) => { raw += c; });
  req.on('end', () => {
    const body = raw ? JSON.parse(raw) : {};
    console.log(req.url, JSON.stringify(body));
    switch (req.url) {
      case '/register': return send(res, 201, { faculty_id: 'f1', name: 'Dr Test Faculty' });
      case '/challenge': n++; return send(res, 201, { challenge_id: `c${n}`, nonce: `n${n}`, expires_in: 60 });
      case '/history': {
        const t = today();
        const past = [
          { day: '2026-11-09', status: 'COMPLETE', worked_s: 21900 },
          { day: '2026-11-02', status: 'COMPLETE', worked_s: 28800 },
          { day: '2026-11-04', status: 'INCOMPLETE', worked_s: null },
          { day: '2026-11-05', status: 'COMPLETE', worked_s: 25200 },
        ];
        const todayEntry = isIn
          ? [{ day: t, status: 'IN_PROGRESS', worked_s: workedS }]
          : workedS > 0 ? [{ day: t, status: 'COMPLETE', worked_s: workedS }] : [];
        const days = [t, '2026-11-09', '2026-11-05', '2026-11-04', '2026-11-03', '2026-11-02'];
        const base = { name: 'Dr Test Faculty', today: t, marked_today: true, window_open: true, days };
        return send(res, 200, v1 ? base : { ...base, is_in: isIn, open_since_ist: openSince, today_worked_s: workedS, day_status: [...todayEntry, ...past] });
      }
      case '/attendance': {
        if (!body.action) return send(res, 201, { day: today(), distance_m: 12 });
        if (body.action === 'in' && isIn) return send(res, 409, { error: 'already_in' });
        if (body.action === 'out' && !isIn) return send(res, 409, { error: 'not_in' });
        const at = istNow();
        if (body.action === 'in') { isIn = true; openSince = at; } else { isIn = false; openSince = null; workedS += 2 * 3600 + 15 * 60; }
        return send(res, 201, { action: body.action, day: today(), at_ist: at, distance_m: 12, is_in: isIn, today_worked_s: workedS });
      }
      case '/mock/v1': v1 = !v1; return send(res, 200, { v1 });
      case '/mock/flip': isIn = !isIn; openSince = isIn ? istNow() : null; return send(res, 200, { isIn });
      default: return send(res, 404, { error: 'not_found' });
    }
  });
}).listen(PORT, () => console.log(`mock api on ${PORT}`));
