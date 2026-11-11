# Infrastructure tasks

Tasks for shared infrastructure and monitoring, including Netdata Community
with Cloud-only access. Track work here even when implementation belongs in an
application repository. Completion requires
verification in production; configuration alone is not evidence of coverage.

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

- [ ] **Configure private PostgreSQL metrics collection in Netdata.**
  Design a private connection path from the host agent to the Swarm service;
  PostgreSQL currently has no published port. Use a dedicated non-superuser
  monitoring role with only required permissions and appropriate TLS. Store
  credentials outside Git. Keep application network isolation and the shared
  database administrator credentials intact. Verify connection, transaction,
  and lock/deadlock metrics and relevant alerts without disrupting applications.
  Document configuration, credential rotation, and rollback.

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
  Removed the old zero-replica service only after those checks; retained the volume,
  old secrets/network, saved service specs and root-only backup for rollback.
  See [the cutover/rotation/rollback runbook](../redis/README.md). Disposable AOF
  preservation tests and the Redis high/critical vulnerability scan also passed.

- [ ] **Configure private Redis metrics collection in Netdata.**
  Shared Redis is now `perry-infra_redis`, reached by approved applications over
  the private encrypted `redis_clients` overlay. Provide a private collector path and a
  dedicated ACL with only commands required by the collector. Keep Redis off public ports and
  credentials out of Git. Verify memory, evictions, connections, and hit/miss
  metrics, then document configuration, credential rotation, and rollback.

- [ ] **Configure private Traefik request metrics in Netdata.**
  Enable a dedicated private metrics entrypoint and a collector connection path
  through an administrator-reviewed infrastructure change. Do not expose the
  Traefik API/dashboard or metrics publicly, broaden the Docker socket proxy
  ACL, or bypass the existing snapshot approval boundary. Verify request counts,
  latency, and 5xx metrics per service where supported. Document alert settings,
  deployment verification, and rollback.

- [x] **Verify Netdata outage and recovery email delivery.**
  Verified on 2026-11-11: the owner stopped only Netdata and received the Cloud
  Unreachable email. After restarting Netdata, the charts returned and the owner
  received the Reachable recovery email. Production applications remained running.
  The agent passed local Cloud-only checks and was claimed/online; the Cloud
  dashboard showed all nine containers. The temporary claim-token file was removed.

- [ ] **Confirm UptimeRobot email delivery.**
  A test notification was requested for Fleet API, but mailbox receipt has not
  been confirmed. Confirm receipt or resend a test after checking the owner alert
  contact and spam folder. Record the result before considering external-monitor
  notification delivery verified; Netdata emails do not prove this separate path.

- [ ] **Recheck personal email settings on the Community plan after the trial.**
  Confirm Critical, Warning, Clear, Reachable, and Unreachable email selections
  remain enabled after the Business trial ends, or after explicitly switching to
  Community. Keep the tested Netdata outage/recovery result above; this is a
  separate check of post-trial feature availability.
