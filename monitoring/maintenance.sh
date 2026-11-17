#!/bin/sh
# Administrator orchestration for an existing installation; never a CI entrypoint.
set -eu
PATH=/usr/sbin:/usr/bin:/sbin:/bin
export PATH
[ "$(id -u)" = 0 ] && [ "$#" = 2 ] || {
  echo 'Usage (as root): maintenance.sh EXPECTED_OLD_FINGERPRINT METRICS_ADMIN_BINARY' >&2
  exit 1
}
root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
broker=/usr/local/libexec/perry-infra-release
snapshot=/opt/perry-infra-release
alert=/etc/netdata/health.d/perry-private-metrics.conf
verifier=$(realpath -- "$2")
[ -x "$verifier" ]
[ "$("$broker" fingerprint "$snapshot")" = "$1" ] || {
  echo 'Installed snapshot changed; review it before maintenance.' >&2
  exit 1
}
# Older host installs have an outdated introductory comment. Ignore comment-only
# differences, while still refusing any change to an actual alert directive.
diff -I '^[[:space:]]*#' "$snapshot/monitoring/collectors/health.conf" "$alert" || {
  echo 'Live alert rules differ from the installed snapshot; review drift first.' >&2
  exit 1
}
[ ! -e "$snapshot/network-maintenance" ]
install -d -m 700 /var/backups/perry-maintenance
backup=$(mktemp -d /var/backups/perry-maintenance/health.XXXXXXXX)
umask 077
# Preserve private installation metadata without displaying its contents.
tar -czpf "$backup/before.tar.gz" -C / opt/perry-infra-release \
  usr/local/libexec/perry-infra-release etc/sudoers.d/perry-infra-release \
  etc/ssh/sshd_config.d/11-perry-infra-release.conf \
  var/lib/infra-release/.ssh/authorized_keys \
  etc/netdata/health.d/perry-private-metrics.conf
printf '%s\n' "$1" > "$backup/fingerprint"
awk '{for (i=1; i<NF; i++) if ($i == "ssh-ed25519") {print $i, $(i+1); exit}}' \
  /var/lib/infra-release/.ssh/authorized_keys > "$backup/release.pub"
echo "Rollback archive: $backup"
echo 'Installing the reviewed snapshot and host alerts; changed service specifications may restart.'
# The installer and broker each use the existing exclusive release lock.
sh "$root/deploy/install.sh" "$broker" "$backup/release.pub"
install -o root -g netdata -m 640 \
  "$snapshot/monitoring/collectors/health.conf" "$alert"
# Start the agent with the installed rules and its existing Cloud identity.
# This restarts only monitoring, not applications; verify rule matching in Cloud.
systemctl restart netdata
fingerprint=$("$broker" fingerprint "$snapshot")
"$broker" "$fingerprint"
[ "$("$broker" fingerprint "$root")" = "$fingerprint" ]
"$verifier" verify "$root"
# Do not reboot on a partial rollout or failed dependency/monitoring check.
echo 'Maintenance checks passed. Review Cloud charts and the effective alert rules.'
