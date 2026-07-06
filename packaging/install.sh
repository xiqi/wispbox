#!/bin/sh
# wispbox installer — Debian 12/13 and Ubuntu 24.04 LTS.
#
# Installs Postfix, Dovecot, SQLite, and OpenDKIM, creates the wispbox
# system user and directory layout, installs wispboxd/wispboxctl and the
# systemd unit, initializes the database, and starts the daemon.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/xiqi/wispbox/main/packaging/install.sh | sudo sh
#   (or, from a source checkout after `make build`:  sudo sh packaging/install.sh)
#
# Safety properties:
#   - Truncation-safe: the whole script is function definitions; the final
#     line calls main. A partially downloaded script executes nothing.
#   - Verified downloads: every fetched artifact is checked against the
#     release's SHA256SUMS before it is installed. Set WISPBOX_SKIP_VERIFY=1
#     only if you understand what you are giving up.
#
# Idempotent: safe to re-run for upgrades.
set -eu

# ----------------------------------------------------------------------
# Release configuration. Downloads come from GitHub Releases: assets are
# published at $WISPBOX_DOWNLOAD_BASE/$WISPBOX_TAG/<asset> by the release
# workflow (.github/workflows/release.yml).
# When run from a source checkout containing ./wispboxd and ./wispboxctl,
# the local binaries are installed instead of downloading.
WISPBOX_REPO="${WISPBOX_REPO:-xiqi/wispbox}"
WISPBOX_VERSION="${WISPBOX_VERSION:-}"
WISPBOX_TAG=""
WISPBOX_DOWNLOAD_BASE="${WISPBOX_DOWNLOAD_BASE:-https://github.com/$WISPBOX_REPO/releases/download}"
WISPBOX_SKIP_VERIFY="${WISPBOX_SKIP_VERIFY:-0}"
INSTALLABLE_RELEASE_HINT="Please try again later, or set WISPBOX_VERSION to another available version."
# ----------------------------------------------------------------------

BIN_DIR=/usr/local/bin
LIB_DIR=/usr/local/lib/wispbox
CONF_DIR=/etc/wispbox
GEN_DIR=$CONF_DIR/generated
DATA_DIR=/var/lib/wispbox
LOG_DIR=/var/log/wispbox

# Working directory for downloads; created in main, removed on exit.
WORK=""
SUMS_READY=0

say()  { printf '\033[1;32m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33mwarning:\033[0m %s\n' "$*"; }
die()  { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

release_assets_needed() {
    arch=$(dpkg --print-architecture)
    [ -x ./wispboxd ] || printf '%s\n' "wispboxd_linux_$arch"
    [ -x ./wispboxctl ] || printf '%s\n' "wispboxctl_linux_$arch"
    [ -f ./packaging/upgrade.sh ] || printf '%s\n' "wispbox-upgrade.sh"
    [ -f ./packaging/systemd/wispboxd.service ] || printf '%s\n' "wispboxd.service"
    [ -f ./packaging/systemd/wispbox-upgrade.service ] || printf '%s\n' "wispbox-upgrade.service"
}

resolve_release() {
    [ -n "$WISPBOX_TAG" ] && return 0

    if [ -n "$WISPBOX_VERSION" ]; then
        case "$WISPBOX_VERSION" in
            v*) WISPBOX_TAG="$WISPBOX_VERSION"; WISPBOX_VERSION="${WISPBOX_VERSION#v}" ;;
            *) WISPBOX_TAG="v$WISPBOX_VERSION" ;;
        esac
        return 0
    fi

    say "resolving latest release"
    latest_json=$(curl -fsSL -H 'Accept: application/vnd.github+json' "https://api.github.com/repos/$WISPBOX_REPO/releases/latest") \
        || die "could not find an installable wispbox release for $WISPBOX_REPO. $INSTALLABLE_RELEASE_HINT"
    WISPBOX_TAG=$(printf '%s' "$latest_json" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)
    [ -n "$WISPBOX_TAG" ] || die "could not read the latest wispbox version for $WISPBOX_REPO. $INSTALLABLE_RELEASE_HINT"
    WISPBOX_VERSION="${WISPBOX_TAG#v}"
}

preflight() {
    [ "$(id -u)" = 0 ] || die "run as root: sudo sh install.sh"

    [ -r /etc/os-release ] || die "cannot detect the operating system"
    . /etc/os-release
    case "${ID:-}-${VERSION_ID:-}" in
        debian-12|debian-13|ubuntu-24.04|ubuntu-26.04) say "detected $PRETTY_NAME" ;;
        *) die "unsupported OS: ${PRETTY_NAME:-unknown}. wispbox v0 supports Debian 12/13 and Ubuntu 24.04/26.04 LTS." ;;
    esac

    command -v systemctl >/dev/null 2>&1 || die "systemd is required"

    arch=$(dpkg --print-architecture)
    case "$arch" in
        amd64|arm64) ;;
        *) die "unsupported CPU architecture: $arch. wispbox release binaries are published for amd64 and arm64." ;;
    esac
}

install_packages() {
    say "installing packages (postfix, dovecot, sqlite, opendkim)"
    export DEBIAN_FRONTEND=noninteractive
    # Preseed Postfix so apt never prompts; wispbox generates the real config.
    echo "postfix postfix/main_mailer_type select No configuration" | debconf-set-selections
    apt-get update -qq
    apt-get install -y -qq \
        postfix \
        dovecot-core dovecot-imapd dovecot-lmtpd dovecot-sqlite \
        sqlite3 opendkim ca-certificates curl sudo >/dev/null
}

setup_user_and_dirs() {
    if ! id wispbox >/dev/null 2>&1; then
        say "creating wispbox system user"
        useradd --system --home-dir $DATA_DIR --shell /usr/sbin/nologin wispbox
    fi
    # OpenDKIM reads signing keys owned by wispbox via group membership.
    usermod -a -G wispbox opendkim 2>/dev/null || true
    # journalctl access for the log viewer.
    usermod -a -G systemd-journal wispbox 2>/dev/null || true

    say "creating directories"
    install -d -o root -g root -m 755 $LIB_DIR
    install -d -o wispbox -g wispbox -m 750 \
        $CONF_DIR $GEN_DIR $GEN_DIR/postfix $GEN_DIR/dovecot $GEN_DIR/opendkim \
        $DATA_DIR $DATA_DIR/certs $DATA_DIR/mail $DATA_DIR/dkim $LOG_DIR

    if [ ! -f $CONF_DIR/wispbox.conf ]; then
        cat > $CONF_DIR/wispbox.conf <<'EOF'
# wispbox daemon configuration. Most settings live in the admin UI;
# this file only pins the runtime environment.
mode = production
EOF
        chown wispbox:wispbox $CONF_DIR/wispbox.conf
        chmod 640 $CONF_DIR/wispbox.conf
    fi
}

# ensure_sums fetches the release's SHA256SUMS once, before the first
# verified download.
ensure_sums() {
    [ "$SUMS_READY" = 1 ] && return 0
    resolve_release
    if [ "$WISPBOX_SKIP_VERIFY" = 1 ]; then
        warn "WISPBOX_SKIP_VERIFY=1 — downloads will NOT be checksum-verified"
        SUMS_READY=1
        return 0
    fi
    url="$WISPBOX_DOWNLOAD_BASE/$WISPBOX_TAG/SHA256SUMS"
    say "fetching release checksums ($WISPBOX_TAG)"
    curl -fsSL -o "$WORK/SHA256SUMS" "$url" \
        || die "wispbox $WISPBOX_TAG is not installable because its checksum file is missing. $INSTALLABLE_RELEASE_HINT"
    SUMS_READY=1
}

validate_release_assets() {
    needed=$(release_assets_needed)
    [ -n "$needed" ] || return 0

    resolve_release
    ensure_sums
    for asset in $needed; do
        if [ "$WISPBOX_SKIP_VERIFY" != 1 ]; then
            grep "  $asset\$" "$WORK/SHA256SUMS" >/dev/null 2>&1 \
                || die "wispbox $WISPBOX_TAG is not installable because $asset is missing from the checksum file. $INSTALLABLE_RELEASE_HINT"
        else
            url="$WISPBOX_DOWNLOAD_BASE/$WISPBOX_TAG/$asset"
            curl -fsIL -o /dev/null "$url" \
                || die "wispbox $WISPBOX_TAG is not installable because $asset is missing. $INSTALLABLE_RELEASE_HINT"
        fi
    done
}

# fetch downloads one release asset into $WORK and verifies its checksum.
fetch() {
    asset=$1
    ensure_sums
    url="$WISPBOX_DOWNLOAD_BASE/$WISPBOX_TAG/$asset"
    say "downloading $asset ($WISPBOX_TAG)"
    curl -fsSL -o "$WORK/$asset" "$url" || die "download failed: $url"
    if [ "$WISPBOX_SKIP_VERIFY" != 1 ]; then
        ( cd "$WORK" && grep "  $asset\$" SHA256SUMS | sha256sum -c - >/dev/null 2>&1 ) \
            || die "checksum mismatch for $asset — refusing to install (corrupted download or tampering)"
    fi
}

install_binaries() {
    for name in wispboxd wispboxctl; do
        if [ -x "./$name" ]; then
            say "installing $name from local checkout"
            install -m 755 "./$name" $BIN_DIR/$name
        else
            arch=$(dpkg --print-architecture) # amd64 | arm64
            fetch "${name}_linux_${arch}"
            install -m 755 "$WORK/${name}_linux_${arch}" $BIN_DIR/$name
        fi
    done
}

init_database_and_config() {
    say "initializing database"
    sudo -u wispbox $BIN_DIR/wispboxd migrate --config $CONF_DIR/wispbox.conf

    say "rendering initial mail server configuration"
    sudo -u wispbox $BIN_DIR/wispboxd config render --write --config $CONF_DIR/wispbox.conf >/dev/null
}

wire_symlink() {
    target=$1 link=$2
    if [ -e "$link" ] && [ ! -L "$link" ]; then
        cp -a "$link" "$link.pre-wispbox" 2>/dev/null || true
    fi
    ln -sf "$target" "$link"
}

wire_mail_stack() {
    say "pointing Postfix at the generated configuration"
    wire_symlink $GEN_DIR/postfix/main.cf   /etc/postfix/main.cf
    wire_symlink $GEN_DIR/postfix/master.cf /etc/postfix/master.cf

    # Postfix's local(8) delivery of root/cron mail needs /etc/aliases.db. The
    # preseed skipped Postfix's own configuration, so build it once here.
    [ -f /etc/aliases ] || printf 'postmaster: root\n' > /etc/aliases
    newaliases 2>/dev/null || true

    say "pointing Dovecot at the generated configuration"
    if [ -f /etc/dovecot/dovecot.conf ] && ! grep -q wispbox /etc/dovecot/dovecot.conf 2>/dev/null; then
        cp -a /etc/dovecot/dovecot.conf /etc/dovecot/dovecot.conf.pre-wispbox
    fi
    cat > /etc/dovecot/dovecot.conf <<EOF
# Managed by wispbox — do not edit. Original saved as dovecot.conf.pre-wispbox
!include $GEN_DIR/dovecot/dovecot.conf
EOF

    # Safety net: wispboxd picks 2.3 vs 2.4 templates by the installed Dovecot
    # version, but the exact config is only truly validated by Dovecot itself.
    # doveconf parses the whole config and exits non-zero on any syntax error,
    # so a wrong directive is caught here instead of silently breaking mail.
    if command -v doveconf >/dev/null 2>&1; then
        if ! doveconf -n >/dev/null 2>"$WORK/doveconf.err"; then
            warn "Dovecot rejected the generated config — mail delivery will not work until this is fixed:"
            sed 's/^/    /' "$WORK/doveconf.err" >&2
            warn "please report this with your 'dovecot --version' at https://github.com/xiqi/wispbox/issues"
        fi
    fi

    say "pointing OpenDKIM at the generated configuration"
    if [ -f /etc/opendkim.conf ] && [ ! -L /etc/opendkim.conf ]; then
        cp -a /etc/opendkim.conf /etc/opendkim.conf.pre-wispbox
    fi
    ln -sf $GEN_DIR/opendkim/opendkim.conf /etc/opendkim.conf
}

install_sudoers() {
    say "installing service control allowlist"
    cat > /etc/sudoers.d/wispbox <<'EOF'
# wispbox: allow the daemon to reload mail services, manage the queue,
# and start the fixed one-click upgrade unit. Generated by the installer.
wispbox ALL=(root) NOPASSWD: /usr/bin/systemctl reload-or-restart postfix, \
    /usr/bin/systemctl reload-or-restart dovecot, \
    /usr/bin/systemctl reload-or-restart opendkim, \
    /usr/bin/systemctl restart postfix, \
    /usr/bin/systemctl restart dovecot, \
    /usr/bin/systemctl restart opendkim, \
    /usr/bin/systemctl start --no-block wispbox-upgrade.service, \
    /usr/sbin/postsuper -d *
EOF
    chmod 440 /etc/sudoers.d/wispbox
    visudo -c -f /etc/sudoers.d/wispbox >/dev/null || die "sudoers validation failed"
}

install_upgrade_helper() {
    say "installing upgrade helper"
    if [ -f ./packaging/upgrade.sh ]; then
        install -m 755 ./packaging/upgrade.sh $LIB_DIR/upgrade.sh
    else
        fetch wispbox-upgrade.sh
        install -m 755 "$WORK/wispbox-upgrade.sh" $LIB_DIR/upgrade.sh
    fi
}

install_systemd_unit() {
    say "installing systemd unit"
    if [ -f ./packaging/systemd/wispboxd.service ]; then
        install -m 644 ./packaging/systemd/wispboxd.service /etc/systemd/system/wispboxd.service
    else
        fetch wispboxd.service
        install -m 644 "$WORK/wispboxd.service" /etc/systemd/system/wispboxd.service
    fi
    if [ -f ./packaging/systemd/wispbox-upgrade.service ]; then
        install -m 644 ./packaging/systemd/wispbox-upgrade.service /etc/systemd/system/wispbox-upgrade.service
    else
        fetch wispbox-upgrade.service
        install -m 644 "$WORK/wispbox-upgrade.service" /etc/systemd/system/wispbox-upgrade.service
    fi
    systemctl daemon-reload
}

start_services() {
    say "starting services"
    # wispboxd goes first: on boot it creates the fallback TLS certificate that
    # Dovecot and Postfix reference, so it must exist before they (re)start.
    # 'restart' (not just 'enable --now') matters because apt already started
    # Postfix/Dovecot with their stock config, and on a re-run for an upgrade
    # the old wispboxd is still running the previous binary.
    systemctl enable wispboxd >/dev/null 2>&1 || true
    systemctl restart wispboxd

    sleep 1
    systemctl is-active --quiet wispboxd || {
        warn "wispboxd did not start; check: journalctl -u wispboxd -n 50"
        exit 1
    }

    for svc in postfix dovecot opendkim; do
        systemctl enable "$svc" >/dev/null 2>&1 || true
        if ! systemctl restart "$svc"; then
            warn "$svc failed to restart; check: journalctl -u $svc -n 50"
        fi
    done
}

print_done() {
    PUBLIC_IP=$(curl -fsS -4 --max-time 5 https://ifconfig.me 2>/dev/null || hostname -I | awk '{print $1}')
    cat <<EOF

  ┌─────────────────────────────────────────────────────────────┐
  │  wispbox is installed and running.                          │
  │                                                             │
  │  Finish setup in your browser:                              │
  │      https://$PUBLIC_IP/setup
  │                                                             │
  │  (Your browser will warn about a temporary self-signed      │
  │   certificate — that disappears once your real certificate  │
  │   is issued during setup.)                                  │
  └─────────────────────────────────────────────────────────────┘

EOF
}

main() {
    WORK=$(mktemp -d) || die "could not create a temp directory"
    trap 'rm -rf "$WORK"' EXIT

    preflight
    validate_release_assets
    install_packages
    setup_user_and_dirs
    install_binaries
    init_database_and_config
    wire_mail_stack
    install_upgrade_helper
    install_sudoers
    install_systemd_unit
    start_services
    print_done
}

# The main call is the LAST line on purpose: if `curl | sh` delivers a
# truncated script, sh either hits a syntax error (cut inside a function) or
# reaches EOF without this call — either way, nothing above has executed.
main "$@"
