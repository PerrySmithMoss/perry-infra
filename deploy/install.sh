#!/bin/sh
# Root installs a reviewed snapshot; CI cannot refresh it or execute repo scripts.
set -eu
cd "$(dirname "$0")/.."
[ "$(id -u)" = 0 ] || { echo 'Run with sudo.' >&2; exit 1; }
: "${1:?Pass the dedicated CI public-key file}"
key=$(cat "$1")
printf '%s\n' "$key" | grep -Eq '^ssh-ed25519 [A-Za-z0-9+/=]+( [^[:cntrl:]]*)?$' || exit 1
[ "$(printf '%s\n' "$key" | wc -l)" -eq 1 ] || exit 1
if ! id infra-release >/dev/null 2>&1; then
  useradd --system --user-group --home-dir /var/lib/infra-release --shell /bin/sh --password '*' infra-release
fi
[ "$(id -nG infra-release)" = infra-release ] || { echo 'Release account has unexpected supplementary groups.' >&2; exit 1; }
install -d -o root -g root -m 755 /opt/perry-infra-release /opt/perry-infra-release/postgres /opt/perry-infra-release/docker-proxy /usr/local/libexec /var/lib/infra-release /var/lib/infra-release/.ssh
install -o root -g root -m 644 docker-compose.stack.yaml /opt/perry-infra-release/
install -o root -g root -m 644 postgres/pg_hba.conf /opt/perry-infra-release/postgres/
install -o root -g root -m 644 docker-proxy/haproxy.cfg /opt/perry-infra-release/docker-proxy/
# Preserve the existing ACME account email, without accepting shell text from CI.
docker service inspect perry-infra_traefik | /usr/bin/python3 -c '
import json,re,sys
value=next(v.removeprefix("ACME_EMAIL=") for v in json.load(sys.stdin)[0]["Spec"]["TaskTemplate"]["ContainerSpec"]["Env"] if v.startswith("ACME_EMAIL="))
assert re.fullmatch(r"[A-Za-z0-9_.+@-]+", value)
print("ACME_EMAIL="+value)
' > /opt/perry-infra-release/deployment.env
chmod 600 /opt/perry-infra-release/deployment.env
install -o root -g root -m 755 deploy/broker.py /usr/local/libexec/perry-infra-release
install -o root -g root -m 755 deploy/ssh-command.py /usr/local/libexec/perry-infra-ssh-command
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
python3 -B deploy/fingerprint.py
