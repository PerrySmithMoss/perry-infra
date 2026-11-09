# Restricted infrastructure deployment

`infra-release` is a dedicated SSH account with no Docker group membership,
interactive shell, SFTP or forwarding. Its root-owned forced command accepts
only `infrastructure <sha256 fingerprint>`. The root broker recalculates the
fingerprint from the installed compose file, HBA file and Docker proxy config.
It deploys only `/opt/perry-infra-release`, using a root-owned ACME email setting.
It cannot execute the checkout's code or accept replacement manifests from CI.

## Installation and cutover

Build from a clean, reviewed checkout in a trusted build environment with
the Go version pinned in `deploy/restricted/go.mod` (Go 1.27.3). From the
repository root:

```sh
artifacts=$(mktemp -d)
GOTOOLCHAIN=local GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
  go -C deploy/restricted build -trimpath -buildvcs=true -o "$artifacts/" ./cmd/...
go version -m "$artifacts/perry-infra-release" "$artifacts/perry-infra-ssh-command" "$artifacts/perry-infra-snapshot"
(cd "$artifacts" && sha256sum perry-infra-release perry-infra-ssh-command perry-infra-snapshot > SHA256SUMS)
```

Check the recorded revision, clean build state, and target architecture in
the Go build metadata. Record the checksums in the trusted build environment.
Copy the executables and reviewed checkout to an administrator-controlled
location on the VPS, then verify `sha256sum -c SHA256SUMS` against the trusted
checksums before installation.

1. For an upgrade, keep a root-only copy of `/opt/perry-infra-release` and
   the current `/usr/local/libexec/perry-infra-release` and
   `perry-infra-ssh-command` executables. From the reviewed checkout, run
   `sudo sh deploy/install.sh /path/to/release.pub /path/to/artifacts` while no
   infrastructure deployment is running. The installer and broker share a
   lock. The snapshot helper validates the existing Traefik ACME email and
   prints the installed fingerprint.
2. Reuse the existing dedicated Ed25519 public key for an upgrade. For a new
   installation, store the dedicated private key in the GitHub `production`
   environment as `INFRA_RELEASE_KEY`. Chinwag, Fleet and VitaTrack use separate
   release accounts. The previous shared `deploy` account is retired.
3. Verify that the infrastructure release account rejects shell commands,
   Docker socket tunnelling, SFTP, extra arguments and forwarding. Verify
   a protected deployment and check public service health.

For rollback, use the owner login during a quiet deployment window. Hold
`/run/lock/perry-infra-release.lock` with `flock`, restore the saved snapshot
with its original ownership and permissions, and replace the two executables
at the same paths using a staged copy and rename. The SSH command, sudo target
and fingerprint are compatible across versions. Repeat the restricted SSH
checks and verify a protected deployment after restoring. Keep system Python
available while the saved Python version is a rollback option.

Infrastructure changes require an administrator to review and install the new
snapshot first. Run
`go -C deploy/restricted run ./cmd/perry-infra-snapshot fingerprint ../..`
from the repository root to print its expected fingerprint. The protected
production workflow runs the image, proxy and certificate checks before
deployment. Its release account can deploy only the installed snapshot.

## Portfolio application releases

See [the restricted portfolio release runbook](portfolio/README.md) for the
Chinwag, Fleet and VitaTrack image-only broker, administrator installation,
protected workflow setup, rollback checks and shared credential retirement.
This is a separate command and does not replace the infrastructure broker.
