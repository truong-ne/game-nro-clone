#!/bin/sh
# Boots the project headless (parsing every script reachable from main.tscn
# and the autoloads). With SMOKE_HOST set, also auto-logs into a running server
# and plays for a few seconds, failing unless the client reports SMOKE_OK.
set -u
cd /client
godot --headless --path /client --import >/tmp/import.log 2>&1 || true
if [ -n "${SMOKE_HOST:-}" ]; then
  out=$(timeout 60 godot --headless --path /client -- --smoke "$SMOKE_HOST" "${SMOKE_USER:-smoke$$}" 2>&1)
else
  out=$(godot --headless --path /client --quit-after 120 2>&1)
fi
echo "$out" | grep -E "SCRIPT ERROR|Parse Error|ERROR:|at: |SMOKE" | head -40
if echo "$out" | grep -qE "SCRIPT ERROR|Parse Error"; then
  echo "CHECK FAILED"; exit 1
fi
if [ -n "${SMOKE_HOST:-}" ] && ! echo "$out" | grep -q "SMOKE_OK"; then
  echo "SMOKE FAILED"; exit 1
fi
echo "CHECK OK"
