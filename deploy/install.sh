#!/bin/sh
# Root installs a reviewed snapshot; CI cannot refresh it or execute repo scripts.
set -eu
PATH=/usr/sbin:/usr/bin:/sbin:/bin
export PATH
[ "$(id -u)" = 0 ] && [ "$#" = 2 ] || {
  echo 'Usage (as root): install.sh PUBLIC_KEY ARTIFACT_DIR' >&2
  exit 1
}
key=$(cat -- "$1")
artifacts=$(CDPATH='' cd -- "$2" && pwd)
cd "$(dirname "$0")/.."
printf '%s\n' "$key" | grep -Eq '^ssh-ed25519 [A-Za-z0-9+/=]+( [^[:cntrl:]]*)?$' || exit 1
[ "$(printf '%s\n' "$key" | wc -l)" -eq 1 ] || exit 1
for command in docker sudo sshd flock; do command -v "$command" >/dev/null; done
for binary in perry-infra-release perry-infra-ssh-command perry-infra-snapshot; do
  [ -f "$artifacts/$binary" ] && [ -x "$artifacts/$binary" ] || {
    echo "Missing executable: $artifacts/$binary" >&2
    exit 1
  }
done
umask 077
exec 9>>/run/lock/perry-infra-release.lock
flock -n 9 || { echo 'An infrastructure release or installation is in progress.' >&2; exit 1; }

install -d -o root -g root -m 755 /usr/local/libexec
staging=$(mktemp -d /usr/local/libexec/.perry-infra-release.XXXXXX)
trap 'rm -rf -- "$staging"' EXIT
trap 'exit 1' HUP INT TERM
for binary in perry-infra-release perry-infra-ssh-command perry-infra-snapshot; do
  install -o root -g root -m 755 -- "$artifacts/$binary" "$staging/$binary"
done

# Read and validate the existing ACME email before updating any installed files.
traefik_env=$(docker service inspect --format '{{json .Spec.TaskTemplate.ContainerSpec.Env}}' perry-infra_traefik)
deployment_env=$(printf '%s\n' "$traefik_env" | "$staging/perry-infra-snapshot" acme-env)
if ! id infra-release >/dev/null 2>&1; then
  useradd --system --user-group --home-dir /var/lib/infra-release --shell /bin/sh --password '*' infra-release
fi
[ "$(id -nG infra-release)" = infra-release ] || { echo 'Release account has unexpected supplementary groups.' >&2; exit 1; }
install -d -o root -g root -m 755 /opt/perry-infra-release /opt/perry-infra-release/postgres /opt/perry-infra-release/docker-proxy /var/lib/infra-release /var/lib/infra-release/.ssh
install -o root -g root -m 644 docker-compose.stack.yaml /opt/perry-infra-release/
install -o root -g root -m 644 postgres/pg_hba.conf /opt/perry-infra-release/postgres/
install -o root -g root -m 644 docker-proxy/haproxy.cfg /opt/perry-infra-release/docker-proxy/
printf '%s\n' "$deployment_env" > "$staging/deployment.env"
install -o root -g root -m 600 "$staging/deployment.env" /opt/perry-infra-release/deployment.env
for binary in perry-infra-release perry-infra-ssh-command; do
  mv -f -- "$staging/$binary" "/usr/local/libexec/$binary"
done
printf 'restrict,command="/usr/local/libexec/perry-infra-ssh-command" %s\n' "$key" > /var/lib/infra-release/.ssh/authorized_keys
chmod 644 /var/lib/infra-release/.ssh/authorized_keys
printf '%s\n' 'infra-release ALL=(root) NOPASSWD: /usr/local/libexec/perry-infra-release *' > /etc/sudoers.d/perry-infra-release
chmod 440 /etc/sudoers.d/perry-infra-release
visudo -cf /etc/sudoers.d/perry-infra-release
cat > /etc/ssh/sshd_config.d/11-perry-infra-release.conf <<'CONFIG'
Match User infra-release
    AuthenticationMethods publickey
    PasswordAuthentication no
    KbdInteractiveAuthentication no
    PermitTTY no
    DisableForwarding yes
    PermitUserRC no
    ForceCommand /usr/local/libexec/perry-infra-ssh-command
Match all
CONFIG
sshd -t
systemctl reload ssh
echo 'Installed infrastructure snapshot:'
"$staging/perry-infra-snapshot" fingerprint /opt/perry-infra-release
