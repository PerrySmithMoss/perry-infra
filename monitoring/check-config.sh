#!/bin/sh
# Also installed as a systemd ExecStartPre guard. Fail closed if config changes.
set -eu
PATH=/usr/sbin:/usr/bin:/sbin:/bin
export PATH
[ "$#" = 1 ] || { echo 'Usage: check-config.sh CONFIG' >&2; exit 1; }
awk '
    /^[[:space:]]*([#;]|$)/ { next }
    {
        line=$0
        sub(/^[[:space:]]+/, "", line)
        sub(/[[:space:]]+$/, "", line)
        if (line ~ /^\[/) {
            section=line
            if (section == "[web]") sections++
            next
        }
        if (section == "[web]" && line ~ /^mode[[:space:]]*=/) {
            modes++
            sub(/^mode[[:space:]]*=[[:space:]]*/, "", line)
            if (line != "none") bad=1
        }
    }
    END { exit !(sections == 1 && modes == 1 && !bad) }
' "$1" || { echo 'Refusing to start: require exactly one [web] mode = none.' >&2; exit 1; }
