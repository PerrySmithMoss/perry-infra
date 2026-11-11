#!/bin/sh
# Administrator-only host installation; never invoked by the infra-release key.
set -eu
PATH=/usr/sbin:/usr/bin:/sbin:/bin
export PATH
umask 077
[ "$(id -u)" = 0 ] && [ "$#" = 0 ] || {
    echo 'Usage: sudo sh monitoring/install.sh' >&2; exit 1;
}
# shellcheck disable=SC1091
. /etc/os-release
[ "$ID" = debian ] && [ "$VERSION_ID" = 12 ] || {
    echo 'This installer supports Debian 12 only.' >&2; exit 1;
}
base=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
sh "$base/check-config.sh" "$base/netdata.conf"
install -d -o root -g root -m 700 /run/perry-netdata
exec 9>/run/perry-netdata/install.lock
flock -n 9 || { echo 'Another monitoring operation is running.' >&2; exit 1; }
# Do not silently replace a separately managed installation or custom config.
if command -v netdata >/dev/null 2>&1 || [ -e /etc/netdata/netdata.conf ]; then
    # Both existence and marker are required; any failure refuses migration.
    # shellcheck disable=SC2015
    [ -f /etc/netdata/netdata.conf ] &&
        grep -q '^# Managed by perry-infra/monitoring\.' /etc/netdata/netdata.conf || {
        echo 'Existing Netdata installation is unmanaged. Review/migrate it first.' >&2; exit 1;
    }
fi
install -d -o root -g root -m 700 /var/backups/perry-monitoring
backup=$(mktemp -d /var/backups/perry-monitoring/install.XXXXXX)
for file in /etc/netdata/netdata.conf /etc/systemd/system/netdata.service.d/cloud-only.conf /usr/local/libexec/perry-netdata-check-config; do
    if [ -e "$file" ]; then cp -a --parents -- "$file" "$backup/"; fi
done
printf 'Rollback configuration backup: %s\n' "$backup"
staging=$(mktemp -d /run/perry-netdata/setup.XXXXXX)
changed=no
cleanup() {
    result=$?
    trap - EXIT
    rm -rf -- "$staging"
    if [ "$result" -ne 0 ] && [ "$changed" = yes ]; then
        systemctl stop netdata.service || :
        echo 'Installation failed; Netdata has been stopped. Existing apps were not redeployed.' >&2
    fi
    exit "$result"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
apt-get update
apt-get install -y curl ca-certificates
curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https'     https://get.netdata.cloud/kickstart.sh -o "$staging/kickstart.sh"
# Save the fetched installer and hash for audit; automatic updates follow stable.
install -o root -g root -m 600 "$staging/kickstart.sh" "$backup/kickstart.sh"
sha256sum "$backup/kickstart.sh" > "$backup/kickstart.sha256"
# Stage the secure config AND startup guard before packages can start the agent.
changed=yes
install -d -o root -g root -m 755 /etc/netdata /usr/local/libexec /etc/systemd/system/netdata.service.d
install -o root -g root -m 644 "$base/netdata.conf" /etc/netdata/netdata.conf
install -o root -g root -m 755 "$base/check-config.sh" /usr/local/libexec/perry-netdata-check-config
install -o root -g root -m 644 "$base/cloud-only.conf" /etc/systemd/system/netdata.service.d/cloud-only.conf
systemctl daemon-reload
# No static/source fallback: config and unit locations must remain predictable.
sh "$staging/kickstart.sh" --native-only --non-interactive     --release-channel stable --auto-update --disable-telemetry
# Restore the managed file if package conffile handling generated a replacement.
install -o root -g root -m 644 "$base/netdata.conf" /etc/netdata/netdata.conf
systemctl daemon-reload
systemctl enable netdata.service
systemctl restart netdata.service
sh "$base/verify.sh" --local-only
printf '\nInstalled securely. Next: sudo bash monitoring/claim.sh, then sudo sh monitoring/verify.sh\n'
