#!/bin/sh
# Root installs a reviewed snapshot; CI cannot refresh it or execute repo scripts.
set -eu
PATH=/usr/sbin:/usr/bin:/sbin:/bin
export PATH
[ "$(id -u)" = 0 ] && [ "$#" = 2 ] || {
  echo 'Usage (as root): install.sh BINARY PUBLIC_KEY' >&2
  exit 1
}
case "$1" in
  /*) binary=$1 ;;
  *) binary=$PWD/$1 ;;
esac
key=$(cat -- "$2")
printf '%s\n' "$key" | grep -Eq '^ssh-ed25519 [A-Za-z0-9+/=]+( [^[:cntrl:]]*)?$' || exit 1
[ "$(printf '%s\n' "$key" | wc -l)" -eq 1 ] || exit 1
cd "$(dirname "$0")/.."
installed=/usr/local/libexec/perry-infra-release
snapshot=/opt/perry-infra-release
lock=/run/lock/perry-infra-release.lock
# Publish a private lock file without opening a path an unprivileged user could
# create in the sticky lock directory. A root-owned lock stays at the same path.
if [ ! -e "$lock" ] && [ ! -L "$lock" ]; then
  candidate=$(mktemp /run/lock/.perry-infra-release.XXXXXX)
  ln -T -- "$candidate" "$lock" 2>/dev/null || :
  rm -f -- "$candidate"
fi
if [ -L "$lock" ] || [ ! -f "$lock" ] || [ "$(stat -c '%u:%a' -- "$lock")" != 0:600 ]; then
  echo 'Expected a root-owned infrastructure release lock with mode 600.' >&2
  exit 1
fi
exec 9< "$lock"
flock -n 9 || { echo 'An infrastructure release or installation is in progress.' >&2; exit 1; }

install -d -o root -g root -m 755 /usr/local/libexec
staging=$(mktemp -d /usr/local/libexec/.perry-infra-release.XXXXXX)
trap 'rm -rf -- "$staging"' EXIT
trap 'exit 1' HUP INT TERM
install -o root -g root -m 755 -- "$binary" "$staging/broker"
"$staging/broker" fingerprint . >/dev/null

# Preserve the existing ACME account email, without accepting shell text from CI.
env_json=$(docker service inspect --format '{{json .Spec.TaskTemplate.ContainerSpec.Env}}' perry-infra_traefik)
printf '%s\n' "$env_json" | "$staging/broker" acme-env > "$staging/deployment.env"
# Restrict sudo before installing a binary that also has local utility commands.
printf '%s ALL=(root) NOPASSWD: %s ^[0-9a-f]{64}$\n' infra-release "$installed" > "$staging/sudoers"
visudo -cf "$staging/sudoers"

if ! id infra-release >/dev/null 2>&1; then
  useradd --system --user-group --home-dir /var/lib/infra-release --shell /bin/sh --password '*' infra-release
fi
[ "$(id -nG infra-release)" = infra-release ] || { echo 'Release account has unexpected supplementary groups.' >&2; exit 1; }
install -d -o root -g root -m 755 "$snapshot" "$snapshot/postgres" "$snapshot/docker-proxy" "$snapshot/redis" /var/lib/infra-release /var/lib/infra-release/.ssh
install -o root -g root -m 644 docker-compose.stack.yaml "$snapshot/"
install -o root -g root -m 644 postgres/pg_hba.conf "$snapshot/postgres/"
install -o root -g root -m 644 docker-proxy/haproxy.cfg "$snapshot/docker-proxy/"
install -o root -g root -m 644 redis/redis.conf "$snapshot/redis/"
install -o root -g root -m 600 "$staging/deployment.env" "$snapshot/deployment.env"
install -o root -g root -m 440 "$staging/sudoers" /etc/sudoers.d/perry-infra-release
visudo -c
mv -f -- "$staging/broker" "$installed"
cat > "$staging/sshd.conf" <<CONFIG
Match User infra-release
    AuthenticationMethods publickey
    PasswordAuthentication no
    KbdInteractiveAuthentication no
    PermitTTY no
    DisableForwarding yes
    PermitUserRC no
    ForceCommand $installed ssh
Match all
CONFIG
install -o root -g root -m 644 "$staging/sshd.conf" /etc/ssh/sshd_config.d/11-perry-infra-release.conf
sshd -t
systemctl reload ssh
printf 'restrict,command="%s ssh" %s\n' "$installed" "$key" > "$staging/authorized_keys"
install -o root -g root -m 644 "$staging/authorized_keys" /var/lib/infra-release/.ssh/authorized_keys
rm -f /usr/local/libexec/perry-infra-ssh-command
echo 'Installed infrastructure snapshot:'
"$installed" fingerprint "$snapshot"
