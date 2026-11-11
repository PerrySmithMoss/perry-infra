#!/bin/bash
# Read secrets from the administrator terminal, never argv, env, or CI logs.
set +x
set -euo pipefail
PATH=/usr/sbin:/usr/bin:/sbin:/bin
export PATH
umask 077
if [[ $EUID != 0 || $# != 0 ]]; then
    echo 'Usage: sudo bash monitoring/claim.sh' >&2; exit 1
fi
install -d -o root -g root -m 700 /run/perry-netdata
exec 9>/run/perry-netdata/install.lock
flock -n 9 || { echo 'Another monitoring operation is running.' >&2; exit 1; }
/usr/local/libexec/perry-netdata-check-config /etc/netdata/netdata.conf
systemctl is-active --quiet netdata.service
state=$(netdatacli aclk-state)
if grep -Eq '^[[:space:]]*Claimed:[[:space:]]*Yes[[:space:]]*$' <<< "$state"; then
    echo 'Agent is already claimed. Refusing to move it to another Space.' >&2; exit 1
fi
if [[ -e /etc/netdata/claim.conf || -L /etc/netdata/claim.conf ]]; then
    echo 'claim.conf already exists. Diagnose the existing claim before replacing credentials.' >&2; exit 1
fi
read -r -s -p 'Netdata claim token (hidden): ' token </dev/tty
printf '\n' >/dev/tty
read -r -p 'Netdata room ID(s), comma-separated: ' rooms </dev/tty
# Reject newlines/config injection; tokens and room IDs need no INI punctuation.
[[ $token =~ ^[A-Za-z0-9._=+/-]+$ && $rooms =~ ^[A-Za-z0-9_-]+(,[A-Za-z0-9_-]+)*$ ]] || {
    echo 'Unexpected token/room format. Nothing written.' >&2; exit 1;
}
staging=$(mktemp /etc/netdata/.claim.XXXXXX)
trap 'unset token rooms; rm -f -- "$staging"' EXIT
printf '[global]\n    url = https://app.netdata.cloud\n    token = %s\n    rooms = %s\n    insecure = no\n' "$token" "$rooms" > "$staging"
unset token rooms
chown root:netdata "$staging"
chmod 640 "$staging"
mv -T -- "$staging" /etc/netdata/claim.conf
netdatacli reload-claiming-state
printf 'Claim submitted. Run sudo sh monitoring/verify.sh after the node appears in Cloud.\n'
