#!/usr/bin/env bash
# Checks that the files shared by the phone server (server/) and the admin server (admin/server/)
# are byte-for-byte identical (SERVER_PLAN.md rule R1). Exit 0 when all match, 1 otherwise.
# Later phases append their new shared files to SHARED.
set -euo pipefail

cd "$(dirname "$0")/.."

A=server
B=admin/server

SHARED=(
  crypto.go
  config_test.go
)

status=0
for f in "${SHARED[@]}"; do
  if [[ ! -f "$A/$f" ]]; then
    echo "MISSING  $A/$f"; status=1; continue
  fi
  if [[ ! -f "$B/$f" ]]; then
    echo "MISSING  $B/$f"; status=1; continue
  fi
  if diff -q "$A/$f" "$B/$f" >/dev/null; then
    echo "same     $f"
  else
    echo "DIFFERS  $f"; status=1
  fi
done

if [[ $status -eq 0 ]]; then echo "check_sync: OK"; else echo "check_sync: FAILED"; fi
exit $status
