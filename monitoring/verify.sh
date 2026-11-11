#!/bin/sh
set -eu
PATH=/usr/sbin:/usr/bin:/sbin:/bin
export PATH
base=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
local_only=no
case "${1:-}" in
    --config-only)
        [ "$#" = 2 ] || exit 1
        exec sh "$base/check-config.sh" "$2" ;;
    --local-only) [ "$#" = 1 ] || exit 1; local_only=yes ;;
    '') [ "$#" = 0 ] || exit 1 ;;
    *) echo 'Usage: verify.sh [--local-only | --config-only CONFIG]' >&2; exit 1 ;;
esac
[ "$(id -u)" = 0 ] || { echo 'Run with sudo for complete socket/process checks.' >&2; exit 1; }
sh "$base/check-config.sh" /etc/netdata/netdata.conf
systemctl is-active --quiet netdata.service
# Check effective running configuration, not only the file waiting on disk.
runtime=$(mktemp)
trap 'rm -f -- "$runtime"' EXIT
netdatacli dumpconfig > "$runtime"
sh "$base/check-config.sh" "$runtime"
listeners=$(ss -H -lnt 'sport = :19999')
[ -z "$listeners" ] || { echo 'FAIL: port 19999 is listening.' >&2; exit 1; }
echo 'PASS: Netdata active, effective web mode none, no TCP listener on 19999.'
if [ "$local_only" = no ]; then
    state=$(netdatacli aclk-state)
    # Both claimed and online checks must succeed.
    # shellcheck disable=SC2015
    printf '%s\n' "$state" | grep -Eq '^[[:space:]]*Claimed:[[:space:]]*Yes[[:space:]]*$' &&
    printf '%s\n' "$state" | grep -Eq '^[[:space:]]*Online:[[:space:]]*Yes[[:space:]]*$' || {
        echo 'FAIL: agent not claimed/online. Check Cloud setup and outbound HTTPS.' >&2; exit 1;
    }
    echo 'PASS: Cloud claimed and online.'
fi
systemctl show netdata.service -p MemoryCurrent -p TasksCurrent
free -h
df -h /
echo 'Still verify externally: port blocked, nine container charts, and real alert emails.'
