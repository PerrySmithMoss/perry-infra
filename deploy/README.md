# Restricted infrastructure deployment

`infra-release` is a dedicated SSH account with no Docker group membership,
interactive shell, SFTP or forwarding. Its root-owned forced command accepts
only `infrastructure <sha256 fingerprint>`. The root broker recalculates the
fingerprint from the installed compose file, HBA file and Docker proxy config.
It deploys only `/opt/perry-infra-release`, using a root-owned ACME email setting.
It cannot execute the checkout's code or accept replacement manifests from CI.

Install a reviewed snapshot with `sudo sh deploy/install.sh ./release.pub`,
using a dedicated Ed25519 public key. Put its private key in the GitHub
`production` environment as `INFRA_RELEASE_KEY`. Verify a protected deployment.
This workflow has no shared-key path. Other applications still using the shared
`deploy` account need their own migration before that VPS key is revoked.

Infrastructure changes require an administrator to review and install the new
snapshot first. `python3 -B deploy/fingerprint.py` prints its expected fingerprint.
The GitHub deployment will deliberately reject an uninstalled snapshot, including
an automatically proposed major PostgreSQL upgrade. No CI key can grant a new
container a host mount, broaden networking or expose infrastructure secrets.

The production workflow runs the image/proxy/certificate checks before deployment
and uses a protected GitHub environment. The legacy deployment path is present
only for migration and fails closed once its old repository key is removed.

## Portfolio application releases

See [the restricted portfolio release runbook](portfolio/README.md) for the
Chinwag, Fleet and VitaTrack image-only broker, administrator installation,
protected workflow setup, rollback checks and shared credential retirement.
This is a separate command and does not replace the infrastructure broker.
