# Private PostgreSQL, Redis and Traefik metrics

These collectors require **administrator installation** in addition to a Swarm
release. The first rollout changes the PostgreSQL HBA config, Redis ACL secret
and network attachments, and restarts Traefik. Reserve a short maintenance window:
database/cache connections and proxy requests can fail during the stop-first
updates, generally for seconds but potentially longer on rollback. Check every
application after updates; Im Listening required an operator restart during the
first rollout, as recorded below. PostgreSQL/Redis volumes, AOF, session data and existing credentials
are preserved. Pause infrastructure workflows and avoid concurrent administrator
changes until verification finishes. Do not deploy the new manifest beforehand.

The configuration is installed and production acceptance passed on 2026-11-12;
the three [tasks](../../docs/tasks.md) are complete. Repeat the checks below after
maintenance; configuration and disposable tests alone do not establish live coverage.

Prepared and tested on 2026-11-12: Go unit/race/vet and shell checks passed;
disposable PostgreSQL TLS/HBA/role, Redis Unix-relay/ACL and Traefik source
isolation/5xx checks passed with repeated numeric samples from all four actual
Netdata collectors. Production verification also passed: the administrator's
privileged verifier checked every required metric context, and Cloud showed
the running agent's charts and relevant alert rules.

## Connection and security boundaries

| Host-only address | Swarm relay socket | Backend | Collector |
| --- | --- | --- | --- |
| `perry-infra_postgres:15432` → `127.0.0.1` | `/run/perry-metrics/15432.sock` | PostgreSQL 5432 | `postgres`, TLS verify-full |
| `127.0.0.1:16379` | `/run/perry-metrics/16379.sock` | Redis 6379 | `redis`, named ACL |
| `127.0.0.1:18082` | `/run/perry-metrics/18082.sock` | Traefik metrics 8082 | `traefik` plus filtered `prometheus` |

Systemd socket activation binds only IPv4 loopback. Its unprivileged
`systemd-socket-proxyd` processes connect to Unix sockets in administrator-created
`/run/perry-metrics` (root:netdata 0750). The stateless HAProxy service creates
mode-0666 sockets **inside that protected directory**, avoiding a host-specific
group ID in the snapshot. Tmpfiles recreates the directory before Docker starts
after boot. No database, relay, metrics, dashboard or Docker API port is published.
Local host processes can reach the loopback sockets; database authentication
remains required. This is a single-node design; review placement before adding
another manager.

The relay alone joins all three monitoring overlays. Each backend joins only its
own additional overlay: `postgres_metrics` (10.42.20.0/24), `redis_metrics`
(10.42.21.0/24), `traefik_metrics` (10.42.22.0/24). They are internal, encrypted,
non-attachable and separate from existing application overlays. The relay has no
Docker socket, application network, database volume or credential mount. Its only
host mount is the socket directory; this is explicitly part of the reviewed
snapshot. The existing Docker API proxy configuration/ACL is unchanged. Netdata
receives no Docker-group membership or new root privilege.

PostgreSQL's `netdata_monitor` has LOGIN, `pg_monitor`, CONNECT on `postgres`, a
three-connection limit, read-only default transactions and bounded queries. It
has no superuser, role/database creation, replication or RLS bypass privileges,
and receives no application table grants. HBA permits it only on its overlay,
only to `postgres`, with TLS/SCRAM; explicit rejects precede local trust and legacy
catch-all rules. Per-table/index collection and the Top Queries function are disabled.
Database-wide connections, transactions, held/waiting locks and deadlocks still
come from shared statistics. Superuser-only WAL/file coverage is intentionally
absent. The native collector's separate Running Queries function can still read
query text from `pg_stat_activity` with `pg_monitor`; keep the Cloud Space restricted
to its owner. The host-only `/etc/hosts` alias preserves the certificate's existing SAN,
so the collector uses `sslmode=verify-full` with the public private-CA certificate
and never falls back to plaintext or skipped verification.

Redis's `netdata` ACL has no key/channel patterns and allows only PING, INFO,
HELLO and CLIENT SETINFO (the actual Go Redis client's authentication/metadata
handshake). AUTH is Redis's authentication operation. Key reads/writes,
enumeration, Lua, slowlog and configuration are denied. Query-text functions are
disabled. The new immutable `redis_users_metrics_v2` secret preserves all existing
application, probe, healthcheck and administrator ACL lines. No application URL,
administrator password, eviction or persistence policy changes. Redis traffic
crosses host loopback/Unix sockets and its encrypted private overlay.

Traefik's metrics exporter uses manual routing on a dedicated entrypoint. Its
file-provider router accepts `/metrics` only on that entrypoint, with an IP allow
list for the monitoring overlay; it ignores untrusted forwarded-source headers.
The API/dashboard remain disabled. Public web/websecure entrypoints have no
metrics router. Service labels are enabled, router labels disabled to bound
cardinality. Netdata's native Traefik collector supplies entrypoint request
counts/rates, 1xx–5xx and average latency; the additional Prometheus job preserves
per-service/status-code counters and duration histograms (including sum/count).
No per-user/request URL labels are enabled. A 5xx series appears only after a
service produces one; its absence on a healthy service is not missing collection.

## Build and test

Use Go 1.27.3, matching the brokers. Binaries and all production credentials stay
outside the checkout:

```sh
go -C monitoring/collectors test -race ./...
go -C monitoring/collectors vet ./...
go -C deploy/infrastructure test -race ./...
go -C deploy/infrastructure vet ./...
METRICS_DOCKER_TEST=1 go -C monitoring/collectors test -run TestDisposableCollectors -v -count=1
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go -C monitoring/collectors build -trimpath -o /tmp/perry-metrics-admin .
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go -C deploy/infrastructure build -trimpath -o /tmp/perry-infra-metrics-release .
```

The optional Docker test uses private disposable networks, a named fixture volume,
generated test TLS and the actual pinned Netdata 2.12.3 image. It has no host bind
mounts or published ports. It checks real PostgreSQL collection, Redis metrics
and ACL denials through the Unix relay, Traefik network rejection, and service
5xx/latency export from an intentionally broken **disposable** backend. CI runs
it before deployment. It never faults a production application.

## First production installation

Review the diff and expected fingerprint. Confirm the three subnets are unused
on the VPS and the host has sufficient memory (about 659 MiB available before
this rollout; measure after installation). The relay is limited to 32 MiB and
each socket proxy to 24 MiB; Netdata collectors add their own measured overhead.
Keep an administrator SSH session open. Transfer the reviewed checkout and two
binaries via the administrator path, compare SHA256 checksums on both machines,
then run **in the administrator's terminal**:

```sh
sudo /absolute/path/perry-metrics-admin install \
  /absolute/path/reviewed-checkout /absolute/path/perry-infra-metrics-release
```

The tool requires root. It does not use Docker group access to bypass sudo, ask
for passwords through chat, or grant the CI account host installation access.
It refuses existing collector files, conflicting host aliases/ports, an existing
monitoring identity, or existing private installation state rather than silently
overwriting them. Do not rerun after a partial failure without inspecting state.

It saves the old snapshot, broker, release SSH/sudo files, host files and absence
inventory in root-only `/var/lib/perry-private-metrics/before.tar.gz`. It generates
distinct 256-bit credentials there, adds the PostgreSQL role, imports the versioned
Redis ACL secret, installs the host units/configs and public CA, and preserves
the existing dedicated infrastructure release key. It then calls `deploy/install.sh`
and the restricted broker, whose fingerprint includes all new runtime assets.
It enables the loopback sockets, restarts Netdata, and runs host verification.
The existing claim/Cloud identity, Cloud-only web guard and application data
are retained. Config files containing credentials are root:netdata 0640; backups,
credentials and potentially sensitive debug output are root-only. Never paste
those files into chat or commit/copy them into this public checkout.

## Production acceptance and alerts

Repeat the checks without another rollout:

```sh
sudo /absolute/path/perry-metrics-admin verify /absolute/path/reviewed-checkout
```

If installation reports a final verification failure, use `diagnose CHECKOUT`
first. It reports only recognized safe diagnoses and snapshot/Cloud status.
Both snapshot fingerprints use the installed infrastructure broker, so no broker
binary is required inside the checkout. The private log itself can contain ACL
inputs and must stay private. The installer can finish deploying successfully
before its last check fails. `verify` reuses
the installed configuration and waits up to one minute for Cloud reconnection.
The standalone collector check provides an explicit shell for the `netdata`
service account's temporary debug terminal; its nologin account setting is retained.

The verification checks Cloud claiming/online state, effective web mode none,
socket isolation, encrypted internal networks, PostgreSQL TLS/hostname and role
privileges, Redis auth/denials, all five public health endpoints and Traefik API
rejection. It runs each real Go collector as `netdata` in a temporary pseudo-terminal
(required for automatic debug-job activation in Netdata 2.12), capturing repeated numeric
samples in private logs; zero values are valid, chart declarations alone are not.
It checks PostgreSQL connections/transactions/held and waiting locks/deadlocks,
Redis memory/evictions/clients/hit rate, and Traefik entrypoint and service request/
latency metrics. INFO contains the underlying hit and miss counters. It does not
force locks, deadlocks, evictions or production 5xx errors.

Also verify in **Netdata Cloud** that these charts update on the existing node,
filter the service-labelled Prometheus charts for each of the five application
services, and inspect the active alert list/evaluation status. The standalone
collector checks prove collection under the agent account; Cloud charts prove
the running agent ingested it. Record both, plus actual rollout interruption,
before checking off any task. From another machine, verify TCP 5432, 6379, 8080,
8082, 15432, 16379, 18082 and 19999 are unreachable on public IPv4 and any IPv6.
Check metrics are absent on public HTTPS `/metrics`, `/api/overview`, `/dashboard/`
without interpreting an application's own response as a Traefik metrics endpoint.

Keep stock PostgreSQL connection utilization (80% warning/90% critical), locks
utilization (20% warning) and deadlock rules, and Redis rejected-connection
health rules. Netdata 2.12.3 provides AOF size charts but no stock AOF health
alert; this configuration adds no AOF write/rewrite failure alert. Inspect the
effective thresholds in the installed package; Netdata stable updates may change
stock rules. `health.conf` adds Redis maxmemory 80%/90% with a two-minute delay,
any eviction as critical (noeviction policy), and
entrypoint 5xx warning when the five-minute average is positive for one minute.
Traefik has no stock alerts. Per-service latency/error charts support diagnosis;
choose per-service latency SLOs after a baseline rather than inventing a shared
latency threshold. Clear is delayed five minutes. Existing owner personal Cloud
Critical/Warning/Clear notifications apply; the previously verified outage/recovery
emails remain separate evidence. Confirm the added rules show as active, with no
parse errors. Do not trigger a production incident just to test notification mail.

## Production rollout, 2026-11-12

The administrator installed the snapshot with fingerprint
`60d3468b7c6eea76a71cf288c75e8cea90e6c892fbe50a9deaa24d2244dd9b79`.
Traefik's update completed at 11:46:29 UTC, Redis at 11:46:32 and PostgreSQL at
11:46:39. Netdata restarted at 11:47:05. The installer then reported a generic
`sh failed` during verification. The private-log diagnostic confirmed that the
last agent check ran before Cloud was claimed/online; the stack installation
had completed. The verifier now waits for Cloud reconnection and labels this
phase explicitly. The administrator reran `diagnose` and `verify` successfully,
without reinstalling or regenerating credentials.

Im Listening's task exited with status 0 during the update window, and its
`on-failure` restart policy left it stopped. It was restored at **11:55:31 UTC**
by scaling to zero, allowing Swarm to process that state, then restoring one
replica. Its full service specification hash was identical before and after;
no release policy refresh, image change or credential change was needed. The
exact outage start was not captured, so the service-update timestamps above do
not establish total application downtime. Check this application's recovery
after any future shared PostgreSQL maintenance; a clean exit does not trigger
its current restart policy.

After recovery, all five public health endpoints returned 200 and all ten
Swarm services had their desired replica. The three monitoring overlays are
internal/encrypted/non-attachable. External IPv4 checks confirmed TCP 5432,
6379, 8080, 8082, 15432, 16379, 18082 and 19999 were unreachable, with 443 reachable
as a baseline. No global host IPv6 address was configured. Public `/metrics`
on all five API hosts returned 404 and exposed no Traefik exporter.

Final privileged acceptance passed Cloud claimed/online status, the reviewed
snapshot fingerprint, PostgreSQL CA/hostname verification and a live TLS session,
non-superuser role limits, Redis authentication/command denials, private API
rejection and repeated numeric samples from all four real collectors. In Cloud,
the existing `my-vps` node showed PostgreSQL connections/transactions/locks/
deadlocks, Redis memory/clients/hit rate/evictions, and Traefik entrypoint metrics.
Both service request and duration series retained all five application service
labels. A controlled disposable 502 proved the status-code/5xx path; no
production 5xx was deliberately induced.

Cloud's Running alerts list showed the PostgreSQL connection, acquired-lock and
per-database deadlock rules as Clear, plus `perry_redis_memory`,
`perry_redis_evictions` and `perry_traefik_5xx` as Clear. These are actual evaluated
rules, not merely files awaiting installation. The separate Cloud Misconfigured
analysis had not been run; no result from that analysis is claimed. All five
health endpoints still returned 200 and all ten services had their desired replicas
at the final 16:57 UTC check. Existing email-delivery and post-trial checks remain
separate tasks.

## Credential rotation

PostgreSQL: use an administrator `psql` session on the current PostgreSQL task
(the mounted `postgres_user` supplies its username), then `\password netdata_monitor`.
Enter a new random password from private storage into its hidden prompts. Do not
put it in command arguments, environment, shell history or SQL echoed to logs.
Use `sudoedit /etc/netdata/go.d/postgres.conf` to update only this monitoring
credential, restart Netdata, then run full verification and check Cloud charts.
Save the current credential in private storage. This briefly interrupts monitoring;
existing application/admin credentials and sessions are unaffected. For rollback,
reset the monitoring role to its saved prior password and restore that collector
file, then restart/reverify. Do not rotate the shared database administrator role.

Redis: build the existing `redis` Go utility. Use the **current** ACL input (read
privately from the live Redis secret if it has changed) and current monitoring
URL, initially `/var/lib/perry-private-metrics/redis-users.acl` and `redis.url`:

```sh
sudo /absolute/path/perry-redis-admin rotate netdata \
  /private/current/users.acl /private/current/redis.url /private/new-monitoring-rotation
```

Import its `users.acl` as a new versioned Swarm secret, update the manifest's Redis
ACL source, and have the administrator install/deploy that reviewed snapshot.
Both password hashes work during this transition; application ACLs are preserved.
Activate the new URL without printing its credential:

```sh
sudo /absolute/path/perry-metrics-admin configure-redis \
  /absolute/path/reviewed-checkout /private/new-monitoring-rotation/url
sudo /absolute/path/perry-metrics-admin verify /absolute/path/reviewed-checkout
```

The helper refuses to activate a hash absent from the deployed ACL. It backs up
the collector config, updates the private current credential inputs, restarts
Netdata and repeats auth/denial checks. Confirm live Cloud charts, then run the
Redis tool's `retire netdata` with that ACL/new URL and another new private output
directory. Deploy its ACL as another versioned secret through the same snapshot
boundary. Verify the new password succeeds and old password fails before removing
unreferenced secrets. Keep both hashes until the new credential works. Before
retirement, rollback by activating the old saved URL; afterward restore the prior
versioned ACL snapshot first. These ACL deployments briefly restart Redis, so use
the same maintenance precautions as the initial installation.

## Rollback

For immediate rollout recovery, while releases remain paused:

```sh
sudo /absolute/path/perry-metrics-admin rollback
```

It stops only Netdata/socket proxies and the stateless metrics relay, restores the
previous host files and administrator-installed infrastructure snapshot under its
lock, validates sudo/SSH configuration and deploys the saved snapshot. It disables
the dedicated PostgreSQL login/grant if created, restarts Netdata and retains all
credentials, immutable secrets and backup material for review. It never restores
database/Redis data from an older copy, deletes a data volume or changes application
passwords. Database/cache/proxy updates can briefly interrupt applications again.
Repeat all five health/session checks, `monitoring/verify.sh` and the restricted
release check afterward. Keep Cloud-only mode none and public 19999 blocked.

This is an immediate rollback, not a merge of later infrastructure/host edits:
the saved `/etc/hosts` and snapshot would overwrite later changes. Review/merge
those first if the rollback window has passed. A partial installation with no
`backup-ready` marker requires administrator inspection; do not discard private
state just to make installation run again. Old Redis secrets remain available
for rollback. Remove unreferenced configs/secrets and private backup material only
after a separate retention decision.

## References

- [Netdata PostgreSQL collector](https://github.com/netdata/netdata/blob/v2.12.3/src/go/plugin/go.d/collector/postgres/integrations/postgresql.md)
- [Netdata Redis collector](https://github.com/netdata/netdata/blob/v2.12.3/src/go/plugin/go.d/collector/redis/integrations/redis.md)
- [Netdata Traefik collector](https://github.com/netdata/netdata/blob/v2.12.3/src/go/plugin/go.d/collector/traefik/integrations/traefik.md)
- [Netdata Prometheus collector](https://github.com/netdata/netdata/blob/v2.12.3/src/go/plugin/go.d/collector/prometheus/integrations/prometheus_endpoint.md)
- [Traefik metrics configuration](https://doc.traefik.io/traefik/reference/install-configuration/observability/metrics/)
- [Redis ACL permissions](https://redis.io/docs/latest/operate/oss_and_stack/management/security/acl/)
