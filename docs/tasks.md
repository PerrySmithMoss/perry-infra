# Infrastructure tasks

Own shared VPS work here, including PostgreSQL, Redis, Swarm networks, release
brokers, access controls and monitoring. Coordinate application changes here
when they are required for an infrastructure rollout. Track application-only
features and dependency remediation in the corresponding application repository.
Completion requires the production verification described by each task.

## Order for the remaining work

1. Finish the administrator's PostgreSQL collector verification below.
   The official 17.12 image deployed successfully on 13 November 2026.
2. Restore Fleet road snapping and verify the provider response and route UI,
   as tracked below.

## Switch to the official PostgreSQL 17 image

**Deployed 13 November 2026; final collector verification pending.** Owner: infrastructure
maintainer. The owner chose to proceed with the official image for PostgreSQL
security fixes and simpler maintenance after reviewing the higher package counts.
The owner accepts a fresh database if needed and has authorized skipping backups.

Use this tested image reference:

```text
postgres:17-bookworm@sha256:40865bc877a289da75963d97d72b59633f639fc5af7a4e856bfa3714cae5b536
```

It resolves to PostgreSQL 17.12 on Linux/amd64, published on 12 November 2026.
The architecture-specific manifest is
`sha256:99d1a998c76487d3abb48b141505bda5dcf8089c0c4cd7be40f75de314fa32f0`.
The [release notes](https://www.postgresql.org/docs/release/17.12/) describe the
engine security fixes and index rebuild considerations.

- [x] Verify the release and registry digest.
- [x] Scan the exact image with Trivy 0.76.1 and a refreshed vulnerability
  database. The 13 November scan reported 3 critical and 111 high OS entries,
  plus 26 high/critical `gosu` entries. The deployed custom 17.11 image reported
  2 critical and 101 high OS entries in the same scan comparison. The official
  image passed the existing gate for OS findings with available fixes.
- [x] Open disposable data initialized by the deployed 17.11 image on 17.12.
  Verify preserved data, extensions, UTF-8, locale and collation metadata.
  Fresh initialization, `citext` comparison and UUID generation also passed.
- [x] Verify BulkBets provisioning, migrations, seed data and a runtime-role
  TLS read. Test the deployed Chinwag and VitaTrack images against disposable
  17.12 data. Registration, sign-in, messaging, nutrition/activity and VitaTrack
  ownership checks passed. Certificate-verified connections and prohibited
  cross-app/plaintext connection checks passed.
- [x] Check live index types against the release notes. No GIN or GiST indexes
  or unconstrained public `bpchar` columns were found in the six databases
  inspected. BulkBets' currency code uses `character(3)`, which the release
  notes exempt from the unconstrained `bpchar` issue.
- [x] Create a focused PR pinning the tested official digest.
  Check the latest branch and live snapshot before editing because network
  isolation, monitoring and the Go release broker changed after the original
  PostgreSQL rollout. Preserve those settings and runtime restrictions.
  Replace the custom-image publishing and `test-no-gosu.sh` assumptions with
  verification suitable for an official-image minor upgrade. Retain the previous
  image for rollback until the new release is verified.
  [PR #31](https://github.com/PerrySmithMoss/perry-infra/pull/31) pins the official
  digest and replaces custom publishing with Go runtime checks. Local upgrade,
  fresh initialization, TLS, role/isolation and private metrics checks passed.
- [x] Rerun relevant checks and the scan in CI and retain full reports with
  the rollout evidence. The local assessment files are under
  `/tmp/pg17-official-check.mIRmh42Q`; temporary files may be cleaned up.
  Keep `gosu` findings visible and document that UID 999 startup skips it.
  SQLite is restored by the official image and was absent from PostgreSQL's
  observed linked libraries. Review the additional utility-package findings
  and the remaining linked `libxml2` findings during PR review. A passing gate
  alone is insufficient to close the vulnerability work.
  The required PR and protected production security jobs passed. Full reports
  across all severities and their artifact links are retained in the
  [production evidence](postgres-official-production-20261113.md).
- [x] Stage and verify the exact administrator-installed snapshot, merge after
  required checks pass, and deploy through the protected production workflow.
  The administrator enters any sudo password in their own terminal and approves
  the production job. Reuse the current volume and verify the running digest,
  database system identifier, extensions and collation after restart.
  Staged at `/tmp/perry-postgres-official-20261113` on the VPS; the checkout and
  staged fingerprint both equal
  `862786146e57f5c336fef30c05f1af275b8f4de3808f562d4561db4ab04ec29a`.
  The administrator installed this fingerprint and approved the
  [protected deployment](https://github.com/PerrySmithMoss/perry-infra/actions/runs/43435963349),
  which passed. Live PostgreSQL 17.12 retained the existing volume, database
  identifier, extensions and collation metadata.
- [ ] Verify BulkBets health and market reads, Chinwag and VitaTrack authenticated
  flows, Im Listening database access and private PostgreSQL monitoring. Confirm
  each client's expected transport and role restrictions. Im Listening and
  monitoring now share this PostgreSQL service and require rollout checks too.
  All application flows, expected transports, role limits and cross-database
  denials passed; the live Netdata PostgreSQL session reconnected over TLS.
  The administrator's numeric collector verification remains pending.
- [ ] Record the deployment, app checks and residual findings. Assign unresolved
  Debian findings an owner and next review date during the rollout review.
  The [runtime risk record](../postgres/README.md) assigns the infrastructure
  maintainer a review on 20 November 2026. Deployment and app results are in the
  [production evidence](postgres-official-production-20261113.md); final collector
  acceptance remains pending.

Done when `perry-infra` pins the official digest on its default branch, the
protected deployment and live consumer checks pass, obsolete custom-build
maintenance is retired, and residual package risks remain visibly tracked.

## Other infrastructure tasks

- [x] **Migrate the infrastructure release broker to Go.**
  The protected production workflow passed through the installed Go broker on
  9 November 2026. The port preserved the reviewed snapshot fingerprint and
  included boundary tests. Subsequent network-isolation deployments also passed.
  [PR #12](https://github.com/PerrySmithMoss/perry-infra/pull/12) ·
  [Production run](https://github.com/PerrySmithMoss/perry-infra/actions/runs/42700548806)

- [x] **Isolate application ingress and database networks.**
  Migrate Chinwag, Fleet, Im Listening and VitaTrack off the shared
  `portfolio_network`, following BulkBets's per-application network separation.
  Inventory each app's actual dependencies first. Give each app an encrypted,
  non-attachable ingress overlay shared only with Traefik, and a separate private
  database overlay where needed, shared only with PostgreSQL, the app and its
  approved migration jobs. Retain shared Traefik and PostgreSQL; explicitly select
  each app's Traefik network and retain separate least-privilege database roles.
  Review Redis connectivity as part of the isolation design: preserve per-app
  ACLs and use per-app Redis overlays if a shared client overlay would retain
  unwanted app-to-app reachability. Keep monitoring/control networks separate
  and publish no database or Redis ports.
  Coordinate app manifests and administrator-installed infrastructure snapshots,
  including runtime configuration fingerprints, installation steps and reviewed
  release-broker policies. Document expected downtime and rollback with retained
  service specifications and data; migrate one app at a time. Remove each app's
  old shared-network attachment after cutover, and retire the shared overlay only
  after verifying that no required dependencies remain.
  Before closing this task, verify in production that public HTTPS routing,
  authentication, core application flows, database/migration access and applicable
  Redis sessions/rate limits work; prove unauthorized cross-app network access
  and cross-database/Redis access are denied. Confirm monitoring, restricted
  image releases and rollback still work, and record the results for every app.
  Production cutover and the full specification rollback drill passed on
  2026-11-12 and were accepted by the owner. Final policies were recorded and
  maintenance was cleared. On 2026-11-12/13 each app's protected production
  workflow passed a real image release, retained-image rollback and restoration,
  with controlled core-flow checks on both images. The infrastructure workflow
  accepted the installed fingerprint and rejected the prior uninstalled one.
  Infrastructure and all four app source-sync PRs passed checks and merged;
  their automatic post-merge deployments passed. All ten services remain 1/1,
  all five HTTPS health URLs return 200 and all five external monitors are Up.
  See the [production CI and rollback evidence](network-isolation-production-20261112.md)
  for workflow links, digests, revisions, application checks and retained audits,
  and the [cutover runbook](../deploy/isolation/README.md) for the original drill.
  Fleet's provider-quota 403 and degraded road snapping remain the accepted
  external exception; live updates, reconnect and vehicle search passed.
  On 2026-11-13 the owner authorized completing the remaining cleanup, ending
  the network rollback window. At 10:07 UTC both old overlays had zero endpoints;
  no service specification or running task on the single-node Swarm referenced
  either ID. At 10:28 UTC both old names and IDs were independently confirmed
  absent; no service or running task referenced them. All ten services remained
  1/1, all five health URLs returned 200, all external monitors were Up and
  Netdata had no active alerts. The protected infrastructure deployment after
  retirement passed, and the owner then ran the standalone sudo verifier
  successfully on 2026-11-13. It confirmed encrypted overlays, exact service
  memberships, selected Traefik networks, unpublished data ports, and all four
  apps' final direct-IP denials and applicable database/migration/Redis checks.
  Independent inspection at 10:50 UTC again found both old overlays absent and
  all ten services 1/1. Production verification is complete and this task is
  closed. Preserve all rollback archives, images, configs, secrets, volumes and
  data; full specification rollback is no longer available, while protected
  image rollback remains. Branch protection and Fleet's accepted provider-quota
  exception are tracked separately below.

- [x] **Implement consistent branch protection across all VPS GitHub repositories.**
  Requested by the owner on 2026-11-13, expanding the Im Listening follow-up.
  Audit effective branch protection and repository/organization rulesets for
  `perry-infra` (`main`), `BulkBets` (`master`), `chinwag` (`master`),
  `logistics-realtime-dashboard` (`main`), `im-listening` (`master`) and
  `VitaTrack` (`master`), all under `PerrySmithMoss`. Reconcile this inventory
  with the live VPS services and deployment workflows before applying changes.
  Require pull requests and the appropriate passing checks for each repository,
  enforce the rules for administrators, and block force-pushes and deletion.
  Preserve stronger existing rules and document any necessary, narrowly scoped
  bypass. Verify the exact check names on current PR runs before requiring them;
  Im Listening's documented baseline includes `verify`. Its production
  environment restricts deployments to `master` and its broker is image-only,
  but its default branch had no protection or ruleset at the initial audit.
  Keep production environments restricted to the intended release branch and
  retain the administrator-installed snapshot and restricted release brokers.
  Record the effective policy and enforcement evidence for every repository,
  including a green PR merge and its normal protected production release;
  demonstrate rejected direct pushes and failing PRs without disrupting the live
  default branches. Close only after all VPS repositories have been verified.
  No repository protection settings were changed during the network cutover.
  Completed separately on 2026-11-13. Im Listening's missing policy was applied;
  the other five policies and all production environment restrictions were
  preserved. All six passed failing-check, unreviewed-push, force-push and
  deletion rejection probes on disposable refs, plus positive merge controls;
  the temporary refs were removed. Six reviewed PRs and their required CI and
  real protected releases passed. Independent image/revision checks matched
  the reviewed merges; all ten services were 1/1 and all five health URLs
  returned 200. BulkBets' existing Go security-gate failure was fixed with Go
  1.27.4 without weakening a check. See the
  [policy, probe, CI and deployment evidence](vps-branch-protection-20261113.md).

- [ ] **Restore Fleet road snapping after the routing-provider quota failure.**
  The accepted network-isolation exception remains: OpenRouteService returns
  HTTP 403 with a quota error, including the app-configured probe on 2026-11-13.
  Review the provider usage/reset and application request volume, restore
  successful snapping, then verify the route UI with live vehicles and stream
  reconnect. Record the provider response and functional result; live SSE and
  vehicle search alone do not prove road snapping works.

- [x] **Add a public health endpoint for Chinwag and external monitoring.**
  Completed on 2026-11-11 in [Chinwag PR #7](https://github.com/PerrySmithMoss/chinwag/pull/7).
  Public `/healthz` supports GET/HEAD, returns 200 with minimal no-store JSON
  (no body for HEAD), and performs no authentication or database work.
  This measures process liveness; existing application authentication and the
  Docker probe remain intact. Image smoke tests and the
  [production release](https://github.com/PerrySmithMoss/chinwag/actions/runs/43102183437)
  passed, followed by public GET/HEAD and protected-route checks.
  [UptimeRobot monitor](https://dashboard.uptimerobot.com/monitors/804640419)
  reports Up with a five-minute interval and owner email alerts enabled.

- [x] **Add a public health endpoint for Vita Track and external monitoring.**
  Completed on 2026-11-11 in [Vita Track PR #9](https://github.com/PerrySmithMoss/VitaTrack/pull/9).
  Public `/healthz` supports GET/HEAD, returns 200 with minimal no-store JSON
  (no body for HEAD), and performs no authentication or database work.
  This measures process liveness; GraphQL CSRF protection, authentication, and
  the Docker GraphQL probe remain intact. Image smoke tests and the
  [production release](https://github.com/PerrySmithMoss/VitaTrack/actions/runs/43102279931)
  passed, followed by public GET/HEAD and GraphQL protection/probe checks.
  [UptimeRobot monitor](https://dashboard.uptimerobot.com/monitors/804640428)
  reports Up with a five-minute interval and owner email alerts enabled.

- [x] **Configure private PostgreSQL metrics collection in Netdata.**
  Design a private connection path from the host agent to the Swarm service;
  PostgreSQL currently has no published port. Use a dedicated non-superuser
  monitoring role with only required permissions and appropriate TLS. Store
  credentials outside Git. Keep application network isolation and the shared
  database administrator credentials intact. Verify connection, transaction,
  and lock/deadlock metrics and relevant alerts without disrupting applications.
  Document configuration, credential rotation, and rollback.
  Completed in production on 2026-11-12. The administrator-installed snapshot
  provides a separate encrypted overlay and host-only relay, with TLS
  `verify-full`, a dedicated non-superuser `pg_monitor` login and restricted HBA.
  Privileged checks passed role limits, the live TLS session and repeated numeric
  connection, transaction, held/waiting-lock and deadlock samples. These charts
  were also verified in Cloud; connection, lock and deadlock alerts were running
  and Clear. Application/admin credentials and data are retained. See the
  [configuration, rotation, rollback and rollout incident record](../monitoring/collectors/README.md).

- [x] **Move Redis into perry-infra as shared infrastructure.**
  Migrate `im-listening_redis` from `PerrySmithMoss/im-listening` into the
  `perry-infra` stack so Redis has an infrastructure-owned lifecycle and other
  applications can use it. Coordinate changes to both repositories and the
  administrator-installed deployment snapshots. Preserve existing Redis data
  and Im Listening sessions; document the cutover, any required downtime, and
  rollback. Provide private network access for approved applications, separate
  application credentials with ACLs restricted to their key prefixes and required
  commands, and documented connection settings and credential rotation. Keep
  Redis off public ports and credentials out of Git. Review shared memory limits,
  persistence, and eviction policy so cache workloads cannot evict sessions.
  Verify Im Listening authentication, sessions, and rate limiting in production,
  plus cross-application ACL isolation and the connection path for another app,
  before removing the old service. Update deployment and monitoring documentation
  to reflect shared ownership.
  Completed in production on 2026-11-11. The administrator installed the matching infrastructure
  snapshot, and the migration verified every unexpired key's content and absolute
  expiry on the retained `im-listening_redis-data` volume. An initial Docker
  zero-replica wait stalled; the API was restored before the corrected Go helper
  resumed the cutover. Shared Redis started at 22:47:48 UTC and the API was healthy
  at 22:49:30 UTC. The existing browser session survived a fresh reload.
  Production checks passed private second-client access, bidirectional ACL denials
  including Lua, deployed session/Google Lua/rate limiter clients, password
  registration/sign-in/rejection, logout revocation, and HTTP 429/Retry-After with
  an isolated test address. The disposable account and test state were cleaned up.
  The restricted application release check passed; the installed infra fingerprint
  matches this checkout. All five public health endpoints returned 200. Redis has
  no published port or host listener, and external TCP 6379 was unreachable.
  Removed the old zero-replica service only after those checks; initially retained
  the old secrets/network for rollback. The owner retired that migration rollback
  window on 2026-11-11: removed the unused overlay and both v1 Redis secrets, 25
  stopped Swarm containers, 67 obsolete cached images and duplicate staging files.
  Retained the live data volume, current/previous images, explicitly allowed image
  fallbacks and root-only credentials/backups. Image storage dropped from 19.04 GB
  to 4.87 GB; all five health endpoints passed afterward. The rollback helper now
  checks the original resource IDs before stopping services. The two unused
  historical VitaTrack volumes require a separate data-retention review.
  See [the cutover/rotation/rollback runbook](../redis/README.md). Disposable AOF
  preservation tests and the Redis high/critical vulnerability scan also passed.

- [x] **Configure private Redis metrics collection in Netdata.**
  Shared Redis is now `perry-infra_redis`, reached by approved applications over
  the private encrypted `redis_clients` overlay. Provide a private collector path and a
  dedicated ACL with only commands required by the collector. Keep Redis off public ports and
  credentials out of Git. Verify memory, evictions, connections, and hit/miss
  metrics, then document configuration, credential rotation, and rollback.
  Completed in production on 2026-11-12. A separate encrypted monitoring overlay
  and host-only relay use a dedicated ACL for INFO/PING and the client's required
  handshake commands. Privileged checks passed authentication, key/enumeration/
  config/slowlog/Lua denials and numeric memory, eviction, connection and hit/miss
  evidence. Cloud showed memory, clients, hit rate and zero evictions; the memory
  and eviction alerts were running and Clear. Existing application ACLs, data,
  AOF and noeviction policy are preserved. See the
  [configuration, rotation, rollback and rollout incident record](../monitoring/collectors/README.md).

- [x] **Configure private Traefik request metrics in Netdata.**
  Enable a dedicated private metrics entrypoint and a collector connection path
  through an administrator-reviewed infrastructure change. Do not expose the
  Traefik API/dashboard or metrics publicly, broaden the Docker socket proxy
  ACL, or bypass the existing snapshot approval boundary. Verify request counts,
  latency, and 5xx metrics per service where supported. Document alert settings,
  deployment verification, and rollback.
  Completed in production on 2026-11-12 through the administrator-installed
  snapshot. The dedicated metrics entrypoint permits only its monitoring overlay;
  no metrics/API/dashboard port is published and the Docker proxy ACL is unchanged.
  Native entrypoint and Prometheus service-labelled request/latency charts were
  verified in Cloud for all five application services. Controlled 5xx export was
  verified on a disposable backend; production 5xx was not deliberately induced.
  The 5xx alert was running and Clear. External port and public metrics-path
  checks passed, as did all five health endpoints. See the
  [alert settings, deployment verification and rollback runbook](../monitoring/collectors/README.md).

- [x] **Verify Netdata outage and recovery email delivery.**
  Verified on 2026-11-11: the owner stopped only Netdata and received the Cloud
  Unreachable email. After restarting Netdata, the charts returned and the owner
  received the Reachable recovery email. Production applications remained running.
  The agent passed local Cloud-only checks and was claimed/online; the Cloud
  dashboard showed all nine containers. The temporary claim-token file was removed.

- [x] **Confirm UptimeRobot email delivery.**
  Verified on 2026-11-12 in the owner's mailbox, including an all-mail search
  covering spam and trash. UptimeRobot's `alert@uptimerobot.com` delivered
  "Monitor is DOWN: Im Listening API" at 11:48 and "Monitor is UP: Im Listening
  API" at 11:58 GMT for [monitor 804640113](https://dashboard.uptimerobot.com/monitors/804640113).
  Both messages were in the inbox. They identify the same HTTP 404 incident,
  starting at 11:48:17 and resolving at 11:58:27 (10 minutes 10 seconds).
  This confirms actual external-monitor outage/recovery delivery independently
  of Netdata. Existing Im Listening test down/up emails from 2026-11-11 at 20:45
  were also found in trash; the earlier Fleet test does not need to be resent.

- [x] **Recheck personal email settings on the Community plan after the trial.**
  Verified on 2026-11-12 after the owner authorized ending the Business trial
  early and switching immediately to Community. Plan & Billing confirmed
  Community was Active and Free forever. Space Settings -> Alerts &
  Notifications retained the enabled personal Email method. User Settings ->
  Notifications retained E-mail enabled and membership of the All nodes Room
  in Perrysmithmoss space, with Critical, Warning, Clear, Reachable and
  Unreachable all selected. No notification selections needed changing.
  The 2026-11-11 outage/recovery delivery test above remains the delivery
  evidence; this check verifies feature availability and retained settings on
  Community, without inducing another outage.
