#!/usr/bin/env bash
# Runs the full Crew Studio stack: Postgres + Azurite file storage (Docker), the Go API + Python runner, and the UI.
#
#   ./run-stack.sh          build everything and serve UI + API at http://localhost:8088
#   ./run-stack.sh --dev    API on :8088 plus the Vite dev server with hot reload at http://localhost:5174
#   ./run-stack.sh --stop   stop the Postgres and Azurite containers (data is kept)
#
# Ctrl+C stops the API (and the dev server). The containers keep running so the next start is fast.
# On the very first start the service prints a one-time link to create the first administrator.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
STUDIO="$ROOT/studio"
VENV_PYTHON="$ROOT/.venv/bin/python"
MODE="${1:-}"

info() { printf '\033[1;36m==>\033[0m %s\n' "$*"; }
die() { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

case "$MODE" in
  "" | --dev | --stop) ;;
  -h | --help) sed -n '2,9p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
  *) die "unknown option '$MODE' (use --dev, --stop or --help)" ;;
esac

if [[ "$MODE" == "--stop" ]]; then
  info "Stopping Postgres and Azurite"
  docker compose -f "$STUDIO/docker-compose.yml" --env-file "$STUDIO/.env" stop
  exit 0
fi

# ---------------------------------------------------------------- prerequisites
for cmd in docker go node npm openssl curl setsid; do
  command -v "$cmd" >/dev/null 2>&1 || die "'$cmd' is not installed"
done
docker info >/dev/null 2>&1 || die "Docker is not running"
[[ -x "$VENV_PYTHON" ]] || die "Python venv not found at $VENV_PYTHON (see QUICKSTART.md)"
"$VENV_PYTHON" -c "import crewai" 2>/dev/null || die "crewai is not installed in .venv (see QUICKSTART.md)"
"$VENV_PYTHON" -c "import crewai_files" 2>/dev/null \
  || die "crewai-files is not installed in .venv (work order images need it): .venv/bin/pip install crewai-files"
[[ -f "$ROOT/.env" ]] || info "No $ROOT/.env yet: add OPENAI_API_KEY / GEMINI_API_KEY / ANTHROPIC_API_KEY before running crews"

port_busy() { (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null; }
port_busy 8088 && die "port 8088 is already in use (is Crew Studio already running?)"
if [[ "$MODE" == "--dev" ]] && port_busy 5174; then
  die "port 5174 is already in use (is the dev server already running?)"
fi

# ---------------------------------------------------------------- studio/.env
if [[ ! -f "$STUDIO/.env" ]]; then
  info "Creating studio/.env with a random database password"
  password="$(openssl rand -hex 16)"
  sed "s/change-me/$password/g" "$STUDIO/.env.example" > "$STUDIO/.env"
  chmod 600 "$STUDIO/.env"
fi

# ---------------------------------------------------------------- Postgres and Azurite (local blob storage)
info "Starting Postgres and Azurite"
docker compose -f "$STUDIO/docker-compose.yml" --env-file "$STUDIO/.env" up -d --wait >/dev/null 2>&1 \
  || die "The containers did not start; run: docker compose -f studio/docker-compose.yml --env-file studio/.env up"

# ---------------------------------------------------------------- builds
info "Building the Go service"
(cd "$STUDIO/server" && GOTOOLCHAIN=local go build -o bin/crewstudio ./cmd/crewstudio)

if [[ ! -d "$STUDIO/web/node_modules" ]]; then
  info "Installing UI dependencies"
  (cd "$STUDIO/web" && npm install --no-fund --no-audit)
fi

if [[ "$MODE" != "--dev" ]]; then
  info "Building the UI"
  (cd "$STUDIO/web" && npm run build >/dev/null)
fi

# ---------------------------------------------------------------- run
# Each child runs in its own process group so stopping it also stops anything it started (npm -> vite)
pids=()
cleanup() {
  trap - INT TERM EXIT
  if ((${#pids[@]})); then
    info "Stopping"
    for pid in "${pids[@]}"; do kill -TERM -- "-$pid" 2>/dev/null || true; done
    wait "${pids[@]}" 2>/dev/null || true
  fi
}
trap cleanup INT TERM EXIT

# Sign-in cookies belong to one host name, so links the service prints (setup, SSO) use the same one as here.
# In dev mode people use the Vite server, so sign-in redirects must come back there.
if [[ "$MODE" == "--dev" ]]; then
  export STUDIO_PUBLIC_URL="${STUDIO_DEV_PUBLIC_URL:-http://localhost:5174}"
elif [[ -z "${STUDIO_PUBLIC_URL:-}" ]] && ! grep -q '^STUDIO_PUBLIC_URL=.' "$STUDIO/.env"; then
  export STUDIO_PUBLIC_URL="http://localhost:8088"
fi

# Migrations run automatically when the service starts
(cd "$STUDIO/server" && exec setsid ./bin/crewstudio -studio-dir ..) &
pids+=($!)

# Wait for the API before announcing it (or bail out if it exited, e.g. on a config error)
for _ in $(seq 1 60); do
  curl -fsS http://127.0.0.1:8088/healthz >/dev/null 2>&1 && break
  kill -0 "${pids[0]}" 2>/dev/null || die "the Go service exited; see the messages above"
  sleep 0.5
done

if [[ "$MODE" == "--dev" ]]; then
  (cd "$STUDIO/web" && exec setsid npm run dev) &
  pids+=($!)
  info "Crew Studio (dev): UI http://localhost:5174  ·  API http://localhost:8088   (Ctrl+C to stop)"
else
  info "Crew Studio: http://localhost:8088   (Ctrl+C to stop)"
fi

wait -n "${pids[@]}"
