#!/usr/bin/env bash
# Starts every UFAS service for a same-network demo. Ctrl+C stops all of them.
# Put this file in the UFAS root and run:  ./start.sh
set -u

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LOGS="$ROOT/logs"
mkdir -p "$LOGS"

PIDS=()

detect_demo_ip() {
  # Prefer the first non-loopback IPv4 from the current active interface.
  # This is for a hotspot / same-network demo where the phone connects to the laptop.
  local ip
  ip="$(hostname -I 2>/dev/null | awk '{for (i=1;i<=NF;i++) if ($i ~ /^[0-9.]+$/ && $i !~ /^127\./) {print $i; exit}}')"
  if [ -z "$ip" ]; then
    ip="$(ip route get 1.1.1.1 2>/dev/null | awk '{print $7; exit}')"
  fi
  if [ -z "$ip" ]; then
    echo "!! Could not detect a LAN IP. Connect to the hotspot or set EXPO_PUBLIC_API_BASE_URL manually." >&2
    echo "   Using fallback: http://127.0.0.1:8080" >&2
    ip="127.0.0.1"
  fi
  printf '%s\n' "$ip"
}

write_demo_env() {
  local ip
  ip="$(detect_demo_ip)"
  printf 'EXPO_PUBLIC_API_BASE_URL=http://%s:8080\n' "$ip" > "$ROOT/app/.env"
  echo "-> demo backend URL: http://$ip:8080"
}

# start <name> <dir> <command...>
# Each service gets its own process group (setsid) so that stopping it also
# stops the child processes spawned by `go run`, `npm run dev` and `npx`.
start() {
  local name="$1" dir="$2"
  shift 2
  if [ ! -d "$dir" ]; then
    echo "!! $name: directory not found: $dir (skipped)"
    return
  fi
  (cd "$dir" && exec setsid "$@" >"$LOGS/$name.log" 2>&1) &
  PIDS+=("$!")
  echo "-> $name started (pid $!), log: logs/$name.log"
}

stop_all() {
  echo
  echo "Stopping all services..."
  for pid in "${PIDS[@]}"; do
    kill -TERM -- "-$pid" 2>/dev/null || kill -TERM "$pid" 2>/dev/null
  done
  sleep 1
  for pid in "${PIDS[@]}"; do
    kill -KILL -- "-$pid" 2>/dev/null
  done
  exit 0
}
trap stop_all INT TERM

# Install JS deps on first run only.
[ -d "$ROOT/admin/server/webui/node_modules" ] || (cd "$ROOT/admin/server/webui" && npm install)
[ -d "$ROOT/app/node_modules" ]                || (cd "$ROOT/app" && npm install)

write_demo_env

start server        "$ROOT/server"              go run . serve
start admin-server  "$ROOT/admin/server"        go run . serve
start admin-webui   "$ROOT/admin/server/webui"  npm run dev -- --host 0.0.0.0

echo
echo "All started. Following logs (Ctrl+C stops everything)."
echo
tail -n 0 -F "$LOGS"/*.log &
PIDS+=("$!")

wait