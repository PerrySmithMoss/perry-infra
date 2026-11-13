# Official PostgreSQL 17.12 rollout — 2026-11-13

The protected deployment completed successfully on 13 November 2026. The live
Linux/amd64 database runs the official PostgreSQL 17.12 Bookworm image using the
existing data volume. Application verification passed; the final administrator
collector verification is pending.

## Reviewed source and deployment

- [Implementation PR #31](https://github.com/PerrySmithMoss/perry-infra/pull/31)
  merged as `d6eb2f170bd5625b9483f404bf6778fa04c5a9dd` after its required
  [CI run](https://github.com/PerrySmithMoss/perry-infra/actions/runs/43435151378)
  passed. The custom Dockerfile, no-gosu script and custom publishing workflow
  were retired.
- The owner installed the reviewed snapshot through `deploy/install.sh` using
  administrator sudo. Independent inspection confirmed fingerprint
  `862786146e57f5c336fef30c05f1af275b8f4de3808f562d4561db4ab04ec29a`.
- The owner approved the normal GitHub `production` job. The
  [protected deployment](https://github.com/PerrySmithMoss/perry-infra/actions/runs/43435963349)
  passed its security job and deployed through the installed Go broker.
  The Swarm update completed at **13:48:47 UTC**.
- No credentials or host privileges were added. The runtime snapshot changed
  only the PostgreSQL image reference; HBA, TLS, monitoring, overlays and
  service restrictions were retained. The owner explicitly requested no backups
  or data-migration plan for this minor update; none was created.

## Live database evidence

| Check | Result |
| --- | --- |
| Service and running image reference | `postgres:17-bookworm@sha256:40865bc877a289da75963d97d72b59633f639fc5af7a4e856bfa3714cae5b536` |
| Running image configuration ID | `sha256:431b070f8300b219ab7e6597e7ebb158df31767739896f53a936cfc749d8437a` |
| Architecture / SQL version | `amd64` / `17.12 (Debian 17.12-1.pgdg12+1)` |
| Existing volume | `perry-infra_postgres_data` at `/var/lib/postgresql/data` |
| Database system identifier before and after | `7508711889620807718` |
| Encoding / locale / collation | UTF8; `en_US.utf8`; libc provider; stored and actual version `2.36`, unchanged across the database inventory |
| Extensions | BulkBets `citext` 1.6, `uuid-ossp` 1.1 and `plpgsql` 1.0; `plpgsql` 1.0 in Chinwag, VitaTrack, Im Listening and postgres; unchanged |
| Runtime restrictions | UID/GID `999:999`, read-only root filesystem, all capabilities dropped, 640 MiB memory, one CPU and 256 PIDs; existing bounded tmpfs mounts |
| Swarm and HTTPS | All ten services 1/1; all five inventory health endpoints HTTP 200 |

The old container finished at **13:47:55.311 UTC**. The new server logged
readiness at **13:48:07.082 UTC**, approximately 12 seconds later. Initial
functional probes overlapped the restart and saw transient registration errors;
the post-readiness reruns below all passed. No database reinitialization or
schema migration occurred.

## Live consumer verification

- **BulkBets:** health returned `A-OK!`; the shows listing and show 1's market,
  divisions and competitors returned populated HTTP 200 responses. The live
  `bulkbets_runtime` session used TLS; its role retained no superuser,
  database-creation, role-creation, replication or row-security bypass privileges.
- **Chinwag:** public GET/HEAD health, unauthenticated rejection, registration,
  rejected incorrect password, sign-in, authenticated profile and message-list
  reads passed. Its deployed `pg` driver connected as `chinwag_app` to `chinwag`
  using its expected private non-TLS path. Role-limit and cross-database denial
  checks passed.
- **VitaTrack:** GET/HEAD health, GraphQL CSRF protection, registration,
  incorrect-password rejection, sign-in, profile, nutrition and workout reads
  passed. Access/refresh-token separation and workout, exercise and nutrition
  ownership checks passed. Its deployed driver connected as `vita_track_app` to
  `vita_track` using its expected private non-TLS path; role-limit and
  cross-database denial checks passed.
- **Im Listening:** public health and its deployed driver's database read passed
  as `im_listening` on `im_listening`, using TLS with its configured CA and
  certificate validation. Excess-role checks, cross-database denials and explicit
  plaintext rejection passed.
- **Private PostgreSQL monitoring:** the `netdata_monitor` role retained its
  limited non-superuser `pg_monitor` membership and connection limit of three.
  Its live `netdata` collector session reconnected over TLS after restart.
  Administrator verification of repeated numeric collector samples is pending.

The one Chinwag and two VitaTrack accounts created for these checks were
removed by exact test email/username, including their cascading test records.
No messages were sent to other users. All ten services and five public health
endpoints passed again after cleanup.

## Security evidence and remaining package risks

Both the PR and production security jobs passed. Each ran disposable
17.11-to-17.12 data-reuse/fresh-initialization tests, TLS/extension/metadata
checks, application-role tests, network isolation, private metrics integration
and release-broker boundary checks. Trivy 0.76.1 retained complete JSON reports
across all severities:

- [PR scan artifact](https://github.com/PerrySmithMoss/perry-infra/actions/runs/43435151378/artifacts/14041998551)
- [Production scan artifact](https://github.com/PerrySmithMoss/perry-infra/actions/runs/43435963349/artifacts/14043323028)

Both PostgreSQL reports contain **3 critical and 111 high OS package entries**,
plus **1 critical and 25 high entries in gosu's embedded Go standard library**.
The existing gate found no fixable high/critical OS entries. These findings
remain accepted and tracked, rather than being treated as resolved by the gate.

The official image restores SQLite and utility packages removed by the custom
image. SQLite was absent from the observed PostgreSQL linked libraries; libxml2,
Kerberos/GSSAPI, LDAP and zlib remain linked dependencies requiring continued
review. The official entrypoint's gosu branch runs only for UID 0; this service
starts directly as UID 999. See the [residual risk record](../postgres/README.md).
Owner: infrastructure maintainer, Perry Smith Moss. Next review:
**20 November 2026**, or sooner if a fixed official image becomes available.

The full downloaded reports and local verification output are retained outside
Git at `/tmp/perry-postgres-rollout-20261113` on the administrator's machine;
the linked CI artifacts are the retained remote evidence. Private collector
diagnostics remain in root-owned storage on the VPS.
