# Infrastructure tasks

Tasks for shared infrastructure and monitoring, including Netdata Community
with Cloud-only access. Track work here even when implementation belongs in an
application repository. Completion requires
verification in production; configuration alone is not evidence of coverage.

- [ ] **Isolate application ingress and database networks.**
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
  either ID. **Remaining before closure:** an administrator must retire only
  these empty, unreferenced overlays and rerun verification. The exact sudo
  command has been provided; password authentication remains with the owner.
  Retain all rollback archives, images, configs, secrets, volumes and app data.

- [ ] **Implement consistent branch protection across all VPS GitHub repositories.**
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
  but its default branch currently has no protection or ruleset.
  Keep production environments restricted to the intended release branch and
  retain the administrator-installed snapshot and restricted release brokers.
  Record the effective policy and enforcement evidence for every repository,
  including a green PR merge and its normal protected production release;
  demonstrate rejected unreviewed/failing changes without disrupting the live
  default branches. Close only after all VPS repositories have been verified.
  No repository protection settings were changed during the network cutover.

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
