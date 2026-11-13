# Official PostgreSQL runtime

The stack pins `postgres:17-bookworm` to the tested multi-platform digest
`sha256:40865bc877a289da75963d97d72b59633f639fc5af7a4e856bfa3714cae5b536`.
On the Linux/amd64 VPS this is PostgreSQL 17.12, released 12 November 2026.
The custom Dockerfile, no-gosu check and custom-image publishing workflow are
retired. The previous image remains available for the disposable upgrade check.

Run from the checkout root with Go 1.27.3 and Docker:

```sh
POSTGRES_DOCKER_TEST=1 go -C postgres test -race -v ./...
go -C postgres vet ./...
```

The test opens disposable data initialized by the deployed custom 17.11 image
using the exact stack image on Linux/amd64. It checks the database system
identifier, preserved rows, extensions, encoding and locale/collation metadata,
then reads, writes and dumps the schema. It also checks fresh initialization,
citext comparisons, UUID generation and certificate-verified TLS. The new image
runs as UID/GID 999 with the production read-only root filesystem, dropped
capabilities, memory/CPU/PID limits and bounded writable tmpfs mounts.
All fixture containers and volumes are disposable and cleaned up after the test.

The required `scan` CI job runs these tests, application-role tests, private
metrics checks and application isolation checks against the pinned official
image. Full vulnerability reports across all severities, including gosu findings, are
uploaded as `infrastructure-vulnerabilities`. The gate rejects fixable
high/critical OS findings. A passing gate does not mean that the image is free
of vulnerabilities.

## Residual package findings

Owner: infrastructure maintainer (Perry Smith Moss). Next review: **20 November
2026**, or sooner if a fixed official image becomes available. Review the weekly
scan artifact and upstream fixes, and update the pinned digest through a PR.

The 13 November pre-rollout Trivy 0.76.1 scan reported 3 critical and 111 high
OS package entries and 26 high/critical entries in the Go standard library
embedded in gosu. No OS high/critical finding had an available Debian fix.
The previous custom image had 2 critical and 101 high OS entries. These counts
are package/advisory entries, including duplicated shared-library advisories,
and do not measure application exploitability.

- **gosu:** restored by the official image. The entrypoint only calls it for
  root startup; this service starts directly as UID 999. Its findings remain
  visible in the full report and are excluded from the OS-only gate.
- **SQLite:** restored by the official image, with six high/critical entries in
  the pre-rollout report. It was absent from the observed PostgreSQL linked
  libraries. Keep it visible rather than modifying the official image again.
- **Utility packages:** util-linux and its libraries, gzip, GnuPG and the other
  installed tools broaden the package inventory compared with the custom
  image. Utilities and server dependencies need separate reachability review;
  package presence alone does not establish a network-reachable PostgreSQL flaw.
- **Server-linked libraries:** libxml2, Kerberos/GSSAPI, LDAP, zlib and other
  Debian dependencies retain unresolved findings. Do not dismiss these using
  the unused-helper argument. Track Debian/upstream fixes and reassess any
  PostgreSQL feature or authentication change that makes them more reachable.

## Rollout status

The owner requested this minor update without backups or a data-migration plan.
Reuse the existing data volume and the existing stop-first update. A database
restart briefly interrupts consumers; no schema or application migration is
required by this image change. Production completion remains pending until the
reviewed snapshot is installed through the administrator path, protected CI
deploys it and every database consumer is verified. See
[the task checklist](../docs/tasks.md) and [snapshot installation](../deploy/README.md).
