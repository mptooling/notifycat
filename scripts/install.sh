#!/bin/sh
# install.sh — Bootstrap a notifycat project directory on any POSIX host.
#
# Quickstart:
#   curl -fsSL https://github.com/mptooling/notifycat/releases/latest/download/install.sh | sh
#
# Environment variables:
#   NOTIFYCAT_VERSION   Release tag to fetch (default: pinned in this script;
#                       stamped to the release tag in each published install.sh)
#   NOTIFYCAT_DIR       Target directory name (default: notifycat)
set -eu

VERSION="${NOTIFYCAT_VERSION:-0.11.0}"
INSTALL_DIR="${NOTIFYCAT_DIR:-notifycat}"
REPO="mptooling/notifycat"
# Each release attaches these files plus a SHA256SUMS manifest as assets.
RELEASE_BASE="https://github.com/${REPO}/releases/download/v${VERSION}"

# Files to download into the project directory.
ARTIFACTS="compose.yaml Caddyfile .env.example mappings.example.yaml notifycat"

# ── helpers ───────────────────────────────────────────────────────────────────

die() { printf 'install: error: %s\n' "$*" >&2; exit 1; }

# Print a confirmation prompt and return 0 for yes, 1 for no.
# Reads from /dev/tty so it works when stdin is a curl pipe.
confirm() {
  printf '%s [y/N] ' "$1"
  if [ -t 0 ]; then
    IFS= read -r _c_ans || _c_ans=""
  elif [ -c /dev/tty ]; then
    IFS= read -r _c_ans </dev/tty || _c_ans=""
  else
    printf '\n'
    die "cannot prompt interactively — run the script directly instead of piping it"
  fi
  case "$_c_ans" in y|Y|yes|YES) return 0 ;; *) return 1 ;; esac
}

# Download a URL to a local path using curl or wget.
fetch() {
  _f_url="$1"; _f_dest="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$_f_url" -o "$_f_dest"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$_f_dest" "$_f_url"
  else
    die "neither curl nor wget found — install one and retry"
  fi
}

# Verify the downloaded artifacts against the release's SHA256SUMS manifest.
# Checks only the files we fetched — SHA256SUMS also lists install.sh, which is
# the running script and not re-downloaded here.
verify_checksums() {
  if command -v sha256sum >/dev/null 2>&1; then
    _v_check="sha256sum -c"
  elif command -v shasum >/dev/null 2>&1; then
    _v_check="shasum -a 256 -c"
  else
    die "neither sha256sum nor shasum found — cannot verify download integrity"
  fi
  for _v_a in $ARTIFACTS; do
    grep " ${_v_a}\$" "${INSTALL_DIR}/SHA256SUMS" \
      | ( cd "$INSTALL_DIR" && $_v_check - >/dev/null 2>&1 ) \
      || die "checksum verification failed for ${_v_a} — aborting"
  done
}

# ── dependency checks ─────────────────────────────────────────────────────────

check_deps() {
  command -v docker >/dev/null 2>&1 || \
    die "Docker is not installed — see https://docs.docker.com/get-docker/"

  docker compose version >/dev/null 2>&1 || \
    die "Docker Compose V2 is required but not found — see https://docs.docker.com/compose/install/"
}

# ── directory setup ───────────────────────────────────────────────────────────

prepare_dir() {
  if [ -d "$INSTALL_DIR" ] && [ -n "$(ls -A "$INSTALL_DIR" 2>/dev/null)" ]; then
    confirm "${INSTALL_DIR} already exists and is not empty — re-download files?" || {
      printf 'Aborted.\n'
      exit 0
    }
  fi
  mkdir -p "$INSTALL_DIR"
}

# ── main ──────────────────────────────────────────────────────────────────────

printf 'Notifycat installer  (version %s)\n\n' "$VERSION"

check_deps

prepare_dir

printf 'Downloading into ./%s/\n' "$INSTALL_DIR"
for artifact in $ARTIFACTS; do
  fetch "${RELEASE_BASE}/${artifact}" "${INSTALL_DIR}/${artifact}"
  printf '  %s\n' "$artifact"
done

fetch "${RELEASE_BASE}/SHA256SUMS" "${INSTALL_DIR}/SHA256SUMS"

printf 'Verifying checksums...\n'
verify_checksums
printf '  ok\n'

chmod +x "${INSTALL_DIR}/notifycat"

printf '\nAll done. Run these three commands to finish setup:\n\n'
printf '  cd %s\n' "$INSTALL_DIR"
printf '  ./notifycat setup          # configure .env and mappings.yaml\n'
printf '  docker compose up -d       # start the stack\n'
printf '  ./notifycat doctor         # run preflight checks\n\n'
