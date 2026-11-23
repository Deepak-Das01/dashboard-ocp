#!/usr/bin/env bash
# Interactive CLI installer for kubeops operators.
#
# Features:
#   - Prompts for install options
#   - Live progress bar + status line
#   - Full transcript: ./kubeops-installer/<YYYY-MM-DD-HHMMSS>-log
#   - Credentials:     ./kubeops-installer/kubeadmin-password
#
# Usage:
#   ./scripts/kubeops-installer.sh
#   ./scripts/install-operators.sh --interactive
#
# Non-interactive / CI:
#   NONINTERACTIVE=1 ./scripts/install-operators.sh
#   AUTH_MODE=oidc LOAD_IMAGES=false ./scripts/kubeops-installer.sh --yes
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

VERSION="${VERSION:-$(tr -d '[:space:]' <"$ROOT/VERSION" 2>/dev/null || echo 1.3.0)}"
INSTALL_DIR="${KUBEOPS_INSTALL_DIR:-$ROOT/kubeops-installer}"
AUTH_MODE="${AUTH_MODE:-oidc}"
LOAD_IMAGES="${LOAD_IMAGES:-true}"
BUILD_OPERATORS="${BUILD_OPERATORS:-true}"
PACK_IMAGES="${PACK_IMAGES:-true}"
ASSUME_YES=false
NO_COLOR="${NO_COLOR:-}"

# Parse flags (env vars still win as defaults before prompts).
while [[ $# -gt 0 ]]; do
  case "$1" in
    -y|--yes) ASSUME_YES=true; shift ;;
    --auth-mode) AUTH_MODE="$2"; shift 2 ;;
    --auth-mode=*) AUTH_MODE="${1#*=}"; shift ;;
    --load-images) LOAD_IMAGES=true; shift ;;
    --no-load-images) LOAD_IMAGES=false; shift ;;
    --build) BUILD_OPERATORS=true; shift ;;
    --no-build) BUILD_OPERATORS=false; shift ;;
    --pack) PACK_IMAGES=true; shift ;;
    --no-pack) PACK_IMAGES=false; shift ;;
    -h|--help)
      sed -n '2,20p' "$0"
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      echo "Try: $0 --help" >&2
      exit 2
      ;;
  esac
done

# ── colors / TTY ─────────────────────────────────────────────────────────────
USE_COLOR=false
if [[ -z "$NO_COLOR" && -t 1 ]]; then
  USE_COLOR=true
fi
if $USE_COLOR; then
  C_RESET=$'\033[0m'
  C_BOLD=$'\033[1m'
  C_DIM=$'\033[2m'
  C_CYAN=$'\033[36m'
  C_GREEN=$'\033[32m'
  C_YELLOW=$'\033[33m'
  C_RED=$'\033[31m'
  C_BLUE=$'\033[34m'
else
  C_RESET= C_BOLD= C_DIM= C_CYAN= C_GREEN= C_YELLOW= C_RED= C_BLUE=
fi

hide_cursor() { [[ -t 1 ]] && printf '\033[?25l' || true; }
show_cursor() { [[ -t 1 ]] && printf '\033[?25h' || true; }
on_exit() {
  show_cursor
}
trap on_exit EXIT
trap 'show_cursor; exit 130' INT TERM

ask() {
  # ask "Prompt" "default"
  local prompt="$1" default="${2:-}" reply
  if $ASSUME_YES; then
    printf '%s\n' "$default"
    return
  fi
  if [[ -n "$default" ]]; then
    read -r -p "${prompt} [${default}]: " reply || true
    printf '%s\n' "${reply:-$default}"
  else
    read -r -p "${prompt}: " reply || true
    printf '%s\n' "$reply"
  fi
}

ask_yn() {
  # ask_yn "Prompt" "Y|N"  → prints true|false
  local prompt="$1" default="${2:-Y}" reply
  local hint="Y/n"
  [[ "$default" =~ ^[Nn] ]] && hint="y/N"
  if $ASSUME_YES; then
    [[ "$default" =~ ^[Yy] ]] && echo true || echo false
    return
  fi
  read -r -p "${prompt} [${hint}]: " reply || true
  reply="${reply:-$default}"
  if [[ "$reply" =~ ^[Yy] ]]; then
    echo true
  else
    echo false
  fi
}

banner() {
  cat <<EOF

${C_BOLD}${C_CYAN}╔══════════════════════════════════════════════════════════╗
║           KubeOps Interactive Installer  v${VERSION}           ║
╚══════════════════════════════════════════════════════════╝${C_RESET}

  Installs kubeops operators, addon controllers, and console.
  Logs + kubeadmin password land in: ${C_BOLD}./kubeops-installer/${C_RESET}

EOF
}

# Ordered progress milestones matched against "==> …" lines from install-operators.sh
# percent|status label|substring to match (case-sensitive after "==> ")
PROGRESS_MILESTONES=(
  '5|Checking cluster access|kubeops operators install'
  '12|Loading dashboard / metrics images|Loading dashboard/metrics images'
  '18|Pulling oauth2-proxy image|Ensuring oauth2-proxy image'
  '28|Building operator binaries|Building kubeops operator binaries'
  '42|Packing operator images|Packing kubeops operator images'
  '50|Applying CRDs|Applying CRDs'
  '58|Applying core RBAC + namespaces|Applying kubeops core RBAC'
  '65|Applying addon controllers|Applying addon-controller namespace'
  '72|Applying console + auth deployments|Applying kubeops-console'
  '78|Waiting for operator pods|Waiting for kubeops operator pods'
  '84|Applying ClusterOIDC (Dex)|Applying ClusterOIDC'
  '88|Waiting for Dex + oauth2 secret|Waiting for Dex'
  '92|Applying KubeDashboard|Applying KubeDashboard'
  '96|Waiting for console Ready|Waiting for KubeDashboard Ready'
  '100|Install complete|KubeDashboard is Ready'
)

CURRENT_PCT=0
CURRENT_STATUS="Starting…"
BAR_WIDTH=36
UI_ACTIVE=false

draw_bar() {
  local pct="$1" status="$2" log_rel="$3"
  local filled empty i
  (( pct < 0 )) && pct=0
  (( pct > 100 )) && pct=100
  filled=$(( pct * BAR_WIDTH / 100 ))
  empty=$(( BAR_WIDTH - filled ))

  local bar=""
  for ((i = 0; i < filled; i++)); do bar+="█"; done
  for ((i = 0; i < empty; i++)); do bar+="░"; done

  if [[ -t 1 ]]; then
    if $UI_ACTIVE; then
      # move up 6 lines and redraw block
      printf '\033[6A\033[J'
    fi
    UI_ACTIVE=true
    printf '%s\n' "${C_DIM}──────────────────────────────────────────────────────────${C_RESET}"
    printf '  %sProgress%s  [%s%s%s] %s%3d%%%s\n' \
      "$C_BOLD" "$C_RESET" "$C_GREEN" "$bar" "$C_RESET" "$C_BOLD" "$pct" "$C_RESET"
    printf '  %sStatus%s    %s\n' "$C_BOLD" "$C_RESET" "$status"
    printf '  %sLog%s       %s\n' "$C_BOLD" "$C_RESET" "$log_rel"
    printf '  %sOutput%s    %s(full detail in log file)%s\n' "$C_BOLD" "$C_RESET" "$C_DIM" "$C_RESET"
    printf '%s\n' "${C_DIM}──────────────────────────────────────────────────────────${C_RESET}"
  else
    printf '[%3d%%] %s\n' "$pct" "$status"
  fi
}

match_progress() {
  local line="$1"
  local entry pct label needle
  [[ "$line" == "==> "* ]] || return 1
  local msg="${line#==> }"
  for entry in "${PROGRESS_MILESTONES[@]}"; do
    IFS='|' read -r pct label needle <<<"$entry"
    if [[ "$msg" == *"$needle"* ]]; then
      CURRENT_PCT="$pct"
      CURRENT_STATUS="$label"
      return 0
    fi
  done
  # Unknown ==> line: keep percent, refresh status from message
  CURRENT_STATUS="$msg"
  return 0
}

fetch_kubeadmin_password() {
  local auth_mode="$1" out_file="$2" url_file="$3"
  local ns secret pass url tries=0
  case "$auth_mode" in
    oidc)
      ns="${AUTH_NS:-kubeops-authentication}"
      secret="kubeadmin-oidc-bootstrap"
      ;;
    *)
      ns="${CONSOLE_NS:-kubeops-console}"
      secret="kubeadmin-bootstrap"
      ;;
  esac

  pass=""
  while (( tries < 30 )); do
    pass="$(kubectl -n "$ns" get secret "$secret" -o jsonpath='{.data.password}' 2>/dev/null | base64 -d 2>/dev/null || true)"
    [[ -n "$pass" ]] && break
    sleep 2
    tries=$((tries + 1))
  done

  url="$(kubectl get kubedashboard kube-dashboard -o jsonpath='{.status.url}' 2>/dev/null || true)"
  if [[ -z "$url" ]]; then
    local node_ip
    node_ip="$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}' 2>/dev/null || true)"
    [[ -n "$node_ip" ]] && url="https://${node_ip}:30443/"
  fi
  printf '%s\n' "${url:-}" >"$url_file"

  umask 077
  cat >"$out_file" <<EOF
# Kube Dashboard login (generated by kubeops-installer)
# $(date -u +%Y-%m-%dT%H:%M:%SZ)
username: kubeadmin
password: ${pass:-NOT_FOUND_YET}
auth_mode: ${auth_mode}
namespace: ${ns}
secret: ${secret}
url: ${url:-https://<node-ip>:30443/}
EOF
  chmod 600 "$out_file"
  [[ -n "$pass" ]]
}

# ── interactive prompts ──────────────────────────────────────────────────────
banner

echo "${C_BOLD}Install options${C_RESET}"
echo

if ! $ASSUME_YES; then
  AUTH_MODE="$(ask "Auth mode (oidc / secret / token)" "$AUTH_MODE")"
  case "$AUTH_MODE" in
    oidc|secret|token) ;;
    *)
      echo "${C_RED}ERROR:${C_RESET} AUTH_MODE must be oidc|secret|token" >&2
      exit 1
      ;;
  esac
  LOAD_IMAGES="$(ask_yn "Load dashboard/metrics images?" "$([[ "$LOAD_IMAGES" == true ]] && echo Y || echo N)")"
  BUILD_OPERATORS="$(ask_yn "Build operator binaries?" "$([[ "$BUILD_OPERATORS" == true ]] && echo Y || echo N)")"
  PACK_IMAGES="$(ask_yn "Pack + import operator images?" "$([[ "$PACK_IMAGES" == true ]] && echo Y || echo N)")"
fi

echo
echo "  Auth mode:         ${C_BOLD}${AUTH_MODE}${C_RESET}"
echo "  Load images:       ${LOAD_IMAGES}"
echo "  Build operators:   ${BUILD_OPERATORS}"
echo "  Pack images:       ${PACK_IMAGES}"
echo "  Output directory:  ${C_BOLD}${INSTALL_DIR}${C_RESET}"
echo

if ! $ASSUME_YES; then
  confirm="$(ask_yn "Proceed with install?" Y)"
  [[ "$confirm" == true ]] || { echo "Aborted."; exit 0; }
fi

mkdir -p "$INSTALL_DIR"
STAMP="$(date +%Y-%m-%d-%H%M%S)"
LOG_FILE="${INSTALL_DIR}/${STAMP}-log"
PASS_FILE="${INSTALL_DIR}/kubeadmin-password"
URL_FILE="${INSTALL_DIR}/console-url"
REL_LOG="./kubeops-installer/${STAMP}-log"

{
  echo "# kubeops interactive install"
  echo "# started: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "# auth_mode=${AUTH_MODE} load_images=${LOAD_IMAGES} build=${BUILD_OPERATORS} pack=${PACK_IMAGES}"
  echo "#"
} >"$LOG_FILE"

echo
echo "${C_GREEN}▶${C_RESET} Starting install…"
echo "  Log file: ${C_BOLD}${REL_LOG}${C_RESET}"
echo
hide_cursor
draw_bar 0 "Preparing…" "$REL_LOG"

INSTALL_EC=0
# Unbuffered-ish pipeline: stdbuf if present, else plain.
RUN_CMD=(
  env
  AUTH_MODE="$AUTH_MODE"
  LOAD_IMAGES="$LOAD_IMAGES"
  BUILD_OPERATORS="$BUILD_OPERATORS"
  PACK_IMAGES="$PACK_IMAGES"
  VERSION="$VERSION"
  "$ROOT/scripts/install-operators.sh"
)

if command -v stdbuf >/dev/null 2>&1; then
  RUN_CMD=(stdbuf -oL -eL "${RUN_CMD[@]}")
fi

set +e
"${RUN_CMD[@]}" 2>&1 | while IFS= read -r line || [[ -n "${line:-}" ]]; do
  # Transcript goes to the log file only — terminal shows the progress UI.
  printf '%s\n' "$line" >>"$LOG_FILE"
  if match_progress "$line"; then
    draw_bar "$CURRENT_PCT" "$CURRENT_STATUS" "$REL_LOG"
  fi
done
INSTALL_EC=${PIPESTATUS[0]}
set -e

show_cursor
UI_ACTIVE=false

if (( INSTALL_EC != 0 )); then
  draw_bar "$CURRENT_PCT" "FAILED (exit ${INSTALL_EC})" "$REL_LOG"
  echo
  echo "${C_RED}Install failed.${C_RESET} See log: ${REL_LOG}"
  echo
  echo "${C_DIM}── last log lines ──${C_RESET}"
  tail -n 20 "$LOG_FILE" 2>/dev/null || true
  echo
  echo "  Full log: ${REL_LOG}"
  exit "$INSTALL_EC"
fi

CURRENT_PCT=100
CURRENT_STATUS="Writing kubeadmin password…"
draw_bar 100 "$CURRENT_STATUS" "$REL_LOG"

if fetch_kubeadmin_password "$AUTH_MODE" "$PASS_FILE" "$URL_FILE"; then
  CURRENT_STATUS="Install complete"
else
  CURRENT_STATUS="Install complete (password secret not ready yet)"
  echo "WARNING: could not read kubeadmin password from cluster yet" >>"$LOG_FILE"
fi
draw_bar 100 "$CURRENT_STATUS" "$REL_LOG"

CONSOLE_URL="$(cat "$URL_FILE" 2>/dev/null || true)"
PASS_VALUE="$(awk -F': ' '/^password:/{print $2; exit}' "$PASS_FILE" 2>/dev/null || true)"

{
  echo "#"
  echo "# finished: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "# password_file=${PASS_FILE}"
  echo "# console_url=${CONSOLE_URL}"
} >>"$LOG_FILE"

cat <<EOF

${C_GREEN}${C_BOLD}────────────────────────────────────────────────────────${C_RESET}
${C_GREEN}${C_BOLD}  KubeOps install finished successfully${C_RESET}
${C_GREEN}${C_BOLD}────────────────────────────────────────────────────────${C_RESET}

  Console:   ${C_BOLD}${CONSOLE_URL:-https://<node-ip>:30443/}${C_RESET}
  Username:  ${C_BOLD}kubeadmin${C_RESET}
  Password:  ${C_BOLD}${PASS_VALUE:-see password file}${C_RESET}

  Log:       ${C_BOLD}${REL_LOG}${C_RESET}
  Password:  ${C_BOLD}./kubeops-installer/kubeadmin-password${C_RESET}

EOF
