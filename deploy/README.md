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
and pause infrastructure deployments while installing the snapshot. The VPS
needs sudo 1.9.10+ for the digest-only sudo rule. From the checkout, run
`sudo sh deploy/install.sh ./perry-infra-release ./release.pub` with the
dedicated Ed25519 public key. An existing installation can reuse the same key.
Put its private key in the GitHub `production` environment as
`INFRA_RELEASE_KEY`, then verify a protected deployment. Chinwag, Fleet and
VitaTrack use separate release accounts. The previous shared `deploy` account
and credential have been retired.

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
