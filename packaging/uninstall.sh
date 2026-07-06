#!/bin/sh
# wispbox uninstaller. Removes the daemon and unwires Postfix/Dovecot.
# Mail data and the control database are kept unless --purge is passed.
set -eu

PURGE=no
[ "${1:-}" = "--purge" ] && PURGE=yes

[ "$(id -u)" = 0 ] || { echo "run as root"; exit 1; }

echo "==> stopping wispboxd"
systemctl disable --now wispboxd 2>/dev/null || true
rm -f /etc/systemd/system/wispboxd.service
systemctl daemon-reload

echo "==> restoring Postfix configuration"
for f in main.cf master.cf; do
    if [ -f /etc/postfix/$f.pre-wispbox ]; then
        rm -f /etc/postfix/$f
        mv /etc/postfix/$f.pre-wispbox /etc/postfix/$f
    elif [ -L /etc/postfix/$f ]; then
        # We symlinked it and there was no original to restore; drop the now
        # dangling link rather than leaving Postfix pointing at nothing.
        target=$(readlink /etc/postfix/$f)
        case "$target" in
            /etc/wispbox/*) rm -f /etc/postfix/$f ;;
        esac
    fi
done

echo "==> restoring Dovecot configuration"
if [ -f /etc/dovecot/dovecot.conf.pre-wispbox ]; then
    mv /etc/dovecot/dovecot.conf.pre-wispbox /etc/dovecot/dovecot.conf
fi

echo "==> restoring OpenDKIM configuration"
if [ -f /etc/opendkim.conf.pre-wispbox ]; then
    rm -f /etc/opendkim.conf
    mv /etc/opendkim.conf.pre-wispbox /etc/opendkim.conf
fi

systemctl try-reload-or-restart postfix dovecot opendkim 2>/dev/null || true

echo "==> removing binaries and sudoers entry"
rm -f /usr/local/bin/wispboxd /usr/local/bin/wispboxctl /etc/sudoers.d/wispbox

if [ "$PURGE" = yes ]; then
    echo "==> purging ALL wispbox data (mail, database, certificates, keys)"
    rm -rf /etc/wispbox /var/lib/wispbox /var/log/wispbox
    userdel wispbox 2>/dev/null || true
else
    echo "kept: /var/lib/wispbox (mail + database), /etc/wispbox (config)"
    echo "re-run with --purge to delete them"
fi

echo "wispbox has been removed. Postfix and Dovecot packages were left installed."
