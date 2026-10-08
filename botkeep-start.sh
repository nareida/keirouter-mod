#!/bin/bash
set -euo pipefail

APP_PORT="${SERVER_PORT:-${PORT:-8080}}"
echo "==> KeiRouter on port $APP_PORT"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
if [ -f "$SCRIPT_DIR/backend/cmd/keirouter/main.go" ]; then
  REPO_ROOT="$SCRIPT_DIR"
elif [ -f "$SCRIPT_DIR/../backend/cmd/keirouter/main.go" ]; then
  REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
else
  REPO_ROOT="$SCRIPT_DIR"
fi

BIN="$REPO_ROOT/keirouter-patched"
if [ ! -x "$BIN" ]; then
  echo "==> Downloading keirouter-patched from GitHub Release..."
  curl -sL "https://github.com/nareida/keirouter-mod/releases/download/v1.0.0/keirouter-patched" -o "$BIN"
  chmod +x "$BIN"
fi

FRONTEND_DIR="$REPO_ROOT/frontend/dist"
if [ ! -d "$FRONTEND_DIR" ]; then
  echo "==> Downloading frontend dist from GitHub Release..."
  mkdir -p "$REPO_ROOT/frontend"
  curl -sL "https://github.com/nareida/keirouter-mod/releases/download/v1.0.0/frontend-dist.tar.gz" -o /tmp/frontend-dist.tar.gz
  tar -xzf /tmp/frontend-dist.tar.gz -C "$REPO_ROOT/frontend"
fi

export KEIROUTER_SERVER__HOST="0.0.0.0"
export KEIROUTER_SERVER__PORT="$APP_PORT"
export KEIROUTER_SECURITY__BIND_LOOPBACK_ONLY=false
export KEIROUTER_HEALTH__ENABLED=false
export KEIROUTER_HEALTH__PROBE_INTERVAL=0
export KEIROUTER_PROVIDER_HEALTH__ENABLED=false

if [ -d "$FRONTEND_DIR" ]; then
  export KEIROUTER_FRONTEND_DIR="$FRONTEND_DIR"
fi

echo "==> Running $BIN on 0.0.0.0:$APP_PORT"
exec "$BIN"
