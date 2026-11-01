#!/bin/sh
# Parse-checks each script on its own (autoload names are reported as unknown
# in this mode, so those lines are filtered out).
cd /client
for f in $(find scripts -name '*.gd'); do
  out=$(godot --headless --path /client --check-only --script "res://$f" 2>&1 \
    | grep -E "SCRIPT ERROR|at: " | grep -vE "Identifier not found: (Assets|Net)" | grep -B1 -E "at: " | grep -v "^--")
  if echo "$out" | grep -q "SCRIPT ERROR"; then echo "== $f"; echo "$out" | head -10; fi
done
echo "done"
