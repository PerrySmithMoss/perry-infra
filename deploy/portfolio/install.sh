#!/bin/sh
# Install ONE dedicated release account from an administrator-reviewed build.
set -eu
PATH=/usr/sbin:/usr/bin:/sbin:/bin
export PATH
[ "$(id -u)" = 0 ] && [ "$#" = 3 ] || {
    echo 'Usage (as root): install.sh chinwag|fleet|vita-track BINARY PUBLIC_KEY' >&2
    exit 1
}
app=$1
case "$app" in chinwag|fleet|vita-track) ;; *) exit 1 ;; esac
account=$app-release
binary=/usr/local/libexec/portfolio-release
home=/var/lib/$account
state=/opt/portfolio-release
key=$(cat -- "$3")
printf '%s\n' "$key" | grep -Eq '^ssh-ed25519 [A-Za-z0-9+/=]+( [^[:cntrl:]]*)?$'
[ "$(printf '%s\n' "$key" | wc -l)" -eq 1 ]
ssh-keygen -lf "$3" >/dev/null
if ! id "$account" >/dev/null 2>&1; then
    useradd --system --user-group --home-dir "$home" --shell /bin/sh --password '*' "$account"
fi
[ "$(id -nG "$account")" = "$account" ]
[ "$(getent passwd "$account" | cut -d: -f6-7)" = "$home:/bin/sh" ]
install -d -o root -g root -m 755 /usr/local/libexec "$home" "$home/.ssh"
install -d -o root -g root -m 700 "$state" "$state/docker"
if [ ! -e "$binary" ]; then
    install -o root -g root -m 755 -- "$2" "$binary"
else
    # Do not replace the shared binary while installing another app's key.
    cmp -s -- "$2" "$binary" || { echo 'Installed binary differs; use administrator upgrade procedure.' >&2; exit 1; }
fi
"$binary" record "$app"

sudo_rule=$(mktemp)
ssh_rule=$(mktemp)
trap 'rm -f "$sudo_rule" "$ssh_rule"' EXIT HUP INT TERM
# sudo 1.9.10+ matches the entire argument string against this regex.
printf '%s ALL=(root) NOPASSWD: %s ^release %s sha256:[a-f0-9]{64} [a-f0-9]{40}$\n' "$account" "$binary" "$app" >"$sudo_rule"
visudo -cf "$sudo_rule"
cat >"$ssh_rule" <<CONFIG
Match User $account
    AuthenticationMethods publickey
    AuthorizedKeysFile $home/.ssh/authorized_keys
    PasswordAuthentication no
    KbdInteractiveAuthentication no
    PermitTTY no
    DisableForwarding yes
    PermitTunnel no
    PermitUserRC no
    ForceCommand $binary ssh
Match all
CONFIG
install -o root -g root -m 440 "$sudo_rule" "/etc/sudoers.d/$account"
install -o root -g root -m 644 "$ssh_rule" "/etc/ssh/sshd_config.d/12-$account.conf"
visudo -c
sshd -t
systemctl reload ssh
keyfile=$(mktemp "$home/.ssh/authorized_keys.XXXXXX")
printf 'restrict,command="%s ssh" %s\n' "$binary" "$key" >"$keyfile"
chmod 644 "$keyfile"
mv -f "$keyfile" "$home/.ssh/authorized_keys"
