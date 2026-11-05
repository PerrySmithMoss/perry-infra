# Restricted portfolio releases

This runbook migrates the three applications still using the shared VPS
`deploy` account. Their new workflows may update only the image of one existing
Swarm service. They cannot create a service, change its networks/secrets/routing,
supply a stack manifest, run a Docker command, or install the broker.

| Repository being migrated | Branch | VPS account | Service |
| --- | --- | --- | --- |
| PerrySmithMoss/chinwag | master | chinwag-release | chinwag_chinwag-api |
| PerrySmithMoss/logistics-realtime-dashboard | main | fleet-release | fleet_fleet-api |
| PerrySmithMoss/VitaTrack | master | vita-track-release | vita-track_vita-track-api |

The other production workflows already have separate release paths and do not
use this new image-only broker:

| Existing workflow | Branch | VPS account | What it deploys |
| --- | --- | --- | --- |
| [BulkBets Pipeline](https://github.com/PerrySmithMoss/BulkBets/blob/master/.github/workflows/pipeline.yaml) | master | bulkbets-release | BulkBets API service (`bulkbets_api`), including its application migrations; successful releases then promote the frontend to Vercel |
| [Deploy to VPS](../../.github/workflows/deploy.yaml) | main | infra-release | The administrator-reviewed `perry-infra` stack: PostgreSQL, Traefik and the Docker API proxy |

See the [BulkBets release broker](https://github.com/PerrySmithMoss/BulkBets/blob/master/deploy/restricted/README.md)
and [infrastructure release broker](../README.md) for their own installation
and recovery procedures.

The release account can only request `release sha256:<64 lower-case hex> <40-character commit SHA>`.
The broker pins the repository name, verifies the image's revision label, and
checks the reviewed service ID and configuration before and after updating the
image. The revision label is a consistency check; control of the app's package
still allows release of arbitrary application code. That code can use its own
mounted secrets and reach peers on the existing shared overlay. Network isolation
between the legacy applications is a separate hardening step.

## Administrator installation and cutover

Migrate one app at a time. The current service must already have an immutable
image digest from the app repository, and the host needs sudo 1.9.10+ and
OpenSSH with `DisableForwarding`. Keep a verified owner administrator session
open throughout. The installer does not restart an application or touch the
legacy account.

1. On a trusted builder, review the source and run
   `go -C deploy/portfolio test -race ./...` (Linux), `go -C deploy/portfolio vet ./...`
   and `sh -n deploy/portfolio/install.sh`. With the pinned Go 1.27.3 toolchain:
   ```sh
   cd deploy/portfolio
   GOTOOLCHAIN=local CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o portfolio-release .
   sha256sum portfolio-release install.sh
   ```
   Transfer the two files over the administrator SSH path. Compare their
   checksums on the VPS. Do not fetch or execute a mutable CI build as root.
2. Review the live service on the host before recording policy. Verify the
   exact app image namespace, route and port, no host binds or Docker socket,
   no published ports, no privileged namespaces, the expected overlay, and
   only the app's own secrets. Use targeted fields when recording evidence
   so the record does not contain environment values. Record the live digest
   and its OCI revision label for rollback.
3. Generate a distinct Ed25519 key per app. On the VPS, run
   `sudo sh ./install.sh chinwag ./portfolio-release ./chinwag-release.pub`
   (substitute the intended app and public key). Only the public key goes
   to the VPS. Check `sudo sshd -T -C user=chinwag-release,host=localhost,addr=127.0.0.1`
   for effective `forcecommand`, `disableforwarding` and `permittty`, and
   `sudo -l -U chinwag-release` for the single restricted command. Confirm the
   account is in no supplementary groups. Stop if any restriction is absent.
4. In the repository, configure `VPS Production` with the default branch as the
   only allowed deployment branch and the owner as required reviewer. Add only
   this app's private key as environment secret `APP_RELEASE_KEY`. Set
   `VPS_HOST` and `VPS_KNOWN_HOSTS` as environment variables. Obtain the pinned
   SSH host key through the existing verified administrator connection.
5. Before merging the workflow, test the new key against the live account:
   plain login, `id`, `docker system dial-stdio`, SFTP, `ssh -W` and remote
   forwarding must fail. A release request with an extra arg, shell metacharacters,
   or a digest qualified with a different app's repository must fail. Test
   direct sudo for a different app as the restricted account; it must fail.
   Check the previously recorded digest through the supported SSH command
   (an idempotent verification with no service update).
6. Merge the new workflow, approve one real release through the protected
   environment and verify the requested digest, exactly one running task, and
   the application's core paths. Chinwag: log in to a controlled account and
   verify messages; Fleet: verify live vehicle data/stream and the routing UI;
   VitaTrack: log in to a controlled account and verify activity/nutrition reads.
   Check shared PostgreSQL, proxy, Traefik and BulkBets health. Record exact
   workflow runs and digests, then dispatch a rollback with the previously
   verified digest and revision, repeat health checks, and promote the new
   digest again. Do not run data-changing tests on uncontrolled accounts.
7. After all three migrations and rollback drills pass, confirm no owner
   automation, cron/systemd jobs, CI scope, or emergency workflow still relies
   on the old key. Verify separate administrator recovery. Remove the old
   `DEPLOY_SSH_PRIVATE_KEY` from every GitHub repository/environment and remove
   the legacy account's authorized keys and Docker/sudo access on the VPS.
   Disable login and terminate existing sessions so cached supplementary group
   membership cannot retain Docker access. Prove the old key fails public-key
   authentication; repeat one routine release for each app without fallback.
   Remove the retired local SSH alias and key copies as part of owner cleanup.

## Rollback and policy changes

Use the repository's manual workflow on its default branch with both
`rollback_digest` and `rollback_revision`. This runs the same image-only checks,
without granting access to Swarm's broader service rollback operation. A
timeout is a failed verification: the Swarm manager may still be converging.
Inspect the service and tasks as administrator before retrying.

For a deliberate service spec change, pause release jobs, review and apply the
change as administrator, then run `sudo /usr/local/libexec/portfolio-release record APP`.
Do not record a changed policy to get around a failed routine release. Policy
updates, broker updates, SSH configuration and disaster recovery stay with the
owner. Keep the previous reviewed binary, installer, policy and SSH
configuration and restore them together if an administrator upgrade fails.
