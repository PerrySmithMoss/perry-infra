# Restricted infrastructure deployment

`infra-release` is a dedicated SSH account with no Docker group membership,
interactive shell, SFTP or forwarding. Its root-owned forced command accepts
only `infrastructure <sha256 fingerprint>`. The root broker recalculates the
fingerprint from the installed compose file, HBA file and Docker proxy config.
It deploys only `/opt/perry-infra-release`, using a root-owned ACME email setting.
It cannot execute the checkout's code or accept replacement manifests from CI.

Build the infrastructure broker from the reviewed checkout on a trusted builder.
For the Linux amd64 VPS, run from the repository root with Go 1.27.3:

```sh
GOTOOLCHAIN=local CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go -C deploy/infrastructure build -trimpath -o "$PWD/perry-infra-release" .
sha256sum perry-infra-release
```

Transfer the binary and reviewed checkout over the administrator SSH path and
compare the binary checksum on the VPS. Keep an administrator SSH session open
and schedule the upgrade between deployments. The installer takes the same
exclusive lock as the broker and exits if a deployment is running. The VPS
needs sudo 1.9.10+ for the digest-only sudo rule. From the checkout, run
`sudo sh deploy/install.sh ./perry-infra-release ./release.pub` with the
dedicated Ed25519 public key. An existing installation can reuse the same key.
Put its private key in the GitHub `production` environment as
`INFRA_RELEASE_KEY`, then verify a protected deployment. Chinwag, Fleet and
VitaTrack use separate release accounts. The previous shared `deploy` account
and credential have been retired.

## Rollback

Before an upgrade, save a root-only archive of the current installation and
preserve its ownership and modes. Include the following paths when present.

- `/opt/perry-infra-release`
- `/usr/local/libexec/perry-infra-release` and `/usr/local/libexec/perry-infra-ssh-command`
- `/etc/sudoers.d/perry-infra-release`
- `/etc/ssh/sshd_config.d/11-perry-infra-release.conf`
- `/var/lib/infra-release/.ssh/authorized_keys`

To roll back, pause releases and use the administrator connection. Hold the
existing root-owned `/run/lock/perry-infra-release.lock` with `flock` throughout
the restore. Restore the saved snapshot first and replace the executables with
staged copies and a rename. Keep the digest-only sudo rule active until the
saved executables are in place. Then restore
the saved sudoers rule, SSH configuration, and authorized keys. If the saved
broker used Python, restore both executables and keep system Python available.
Run `visudo -c` and `sshd -t` before reloading SSH. Release the lock, verify
the restricted SSH account and run a protected deployment.

## Snapshot verification

Infrastructure changes require an administrator to review and install the new
snapshot first. Run `go -C deploy/infrastructure run . fingerprint ../..` from
the checkout root to print its expected fingerprint.
The GitHub deployment will deliberately reject an uninstalled snapshot, including
an automatically proposed major PostgreSQL upgrade. No CI key can grant a new
container a host mount, broaden networking or expose infrastructure secrets.

The production workflow runs the image/proxy/certificate checks before deployment
and uses the protected GitHub environment for administrator-reviewed snapshots.

## Portfolio application releases

See [the restricted portfolio release runbook](portfolio/README.md) for the
Chinwag, Fleet and VitaTrack image-only broker, administrator installation,
protected workflow setup, rollback checks and shared credential retirement.
This is a separate command and does not replace the infrastructure broker.
