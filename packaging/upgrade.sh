#!/bin/sh
# Root-only helper run by wispbox-upgrade.service. It resolves the latest
# GitHub release, runs that release's installer, and writes a small status file
# for the admin UI.
set -eu

DATA_DIR="${WISPBOX_DATA_DIR:-/var/lib/wispbox}"
LOG_DIR="${WISPBOX_LOG_DIR:-/var/log/wispbox}"
STATUS="$DATA_DIR/upgrade.json"
LOG="$LOG_DIR/upgrade.log"
LOCK="$DATA_DIR/upgrade.lock"
REPO="${WISPBOX_REPO:-xiqi/wispbox}"
DOWNLOAD_BASE="${WISPBOX_DOWNLOAD_BASE:-https://github.com/$REPO/releases/download}"
STARTED_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
TARGET_VERSION="${WISPBOX_UPGRADE_VERSION:-}"
DONE=0

json_escape() {
    printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g; s/$/\\n/' | tr -d '\n' | sed 's/\\n$//'
}

write_status() {
    state=$1
    target=$2
    message=$3
    finished=${4:-}
    tmp="$STATUS.tmp"
    printf '{"state":"%s","target_version":"%s","started_at":"%s","finished_at":"%s","message":"%s"}\n' \
        "$(json_escape "$state")" \
        "$(json_escape "$target")" \
        "$(json_escape "$STARTED_AT")" \
        "$(json_escape "$finished")" \
        "$(json_escape "$message")" > "$tmp"
    mv "$tmp" "$STATUS"
    chmod 644 "$STATUS" 2>/dev/null || true
}

cleanup() {
    code=$?
    rmdir "$LOCK" 2>/dev/null || true
    if [ "$code" -ne 0 ] && [ "$DONE" -ne 1 ]; then
        write_status failed "$TARGET_VERSION" "Upgrade failed. See the log for details." "$(date -u +%Y-%m-%dT%H:%M:%SZ)" || true
    fi
    exit "$code"
}

mkdir -p "$DATA_DIR" "$LOG_DIR"
touch "$LOG"
chown root:wispbox "$LOG" 2>/dev/null || true
chmod 640 "$LOG" 2>/dev/null || true

if ! mkdir "$LOCK" 2>/dev/null; then
    write_status running "$TARGET_VERSION" "Upgrade already running." ""
    exit 0
fi
trap cleanup EXIT INT TERM

exec >>"$LOG" 2>&1

echo
echo "==> $(date -u +%Y-%m-%dT%H:%M:%SZ) starting wispbox upgrade"
write_status running "$TARGET_VERSION" "Resolving latest release..." ""

if [ -n "$TARGET_VERSION" ]; then
    case "$TARGET_VERSION" in
        v*) TAG="$TARGET_VERSION"; TARGET_VERSION="${TARGET_VERSION#v}" ;;
        *) TAG="v$TARGET_VERSION" ;;
    esac
else
    latest_json=$(curl -fsSL -H 'Accept: application/vnd.github+json' "https://api.github.com/repos/$REPO/releases/latest")
    TAG=$(printf '%s' "$latest_json" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)
    [ -n "$TAG" ] || { echo "could not resolve latest release tag"; exit 1; }
    TARGET_VERSION="${TAG#v}"
fi

write_status running "$TARGET_VERSION" "Downloading installer for $TAG..." ""
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"; cleanup' EXIT INT TERM

install_url="https://raw.githubusercontent.com/$REPO/$TAG/packaging/install.sh"
curl -fsSL -o "$WORK/install.sh" "$install_url"

write_status running "$TARGET_VERSION" "Installing $TAG..." ""
WISPBOX_VERSION="$TARGET_VERSION" WISPBOX_DOWNLOAD_BASE="$DOWNLOAD_BASE" sh "$WORK/install.sh"

write_status succeeded "$TARGET_VERSION" "Upgrade to $TAG completed." "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
DONE=1
echo "==> $(date -u +%Y-%m-%dT%H:%M:%SZ) upgrade to $TAG completed"
