#!/bin/bash
set -euo pipefail

APP_PORT="${SERVER_PORT:-${PORT:-8080}}"
echo "==> KeiRouter on port $APP_PORT"

# Determine repo root (script may live in repo root or backend/)
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
if [ -f "$SCRIPT_DIR/backend/cmd/keirouter/main.go" ]; then
  REPO_ROOT="$SCRIPT_DIR"
elif [ -f "$SCRIPT_DIR/../backend/cmd/keirouter/main.go" ]; then
  REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
else
  echo "ERROR: cannot find backend/cmd/keirouter/main.go from $SCRIPT_DIR"
  exit 1
fi

BIN="$REPO_ROOT/keirouter-patched"
if [ ! -x "$BIN" ]; then
  echo "==> Building keirouter..."
  (cd "$REPO_ROOT/backend" && go build -o "$BIN" ./cmd/keirouter)
fi

export KEIROUTER_SERVER__HOST="0.0.0.0"
export KEIROUTER_SERVER__PORT="$APP_PORT"
export KEIROUTER_SECURITY__BIND_LOOPBACK_ONLY=false
export KEIROUTER_HEALTH__ENABLED=false
export KEIROUTER_HEALTH__PROBE_INTERVAL=0
export KEIROUTER_PROVIDER_HEALTH__ENABLED=false

if [ -d "$REPO_ROOT/frontend/dist" ]; then
  export KEIROUTER_FRONTEND_DIR="$REPO_ROOT/frontend/dist"
fi

echo "==> Running $BIN on 0.0.0.0:$APP_PORT"
exec "$BIN"
