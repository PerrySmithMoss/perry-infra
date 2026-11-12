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
  Implementation is prepared in the infrastructure and four application
  checkouts; the owner started production preparation on 2026-11-12. Shared
  service interfaces were added; the first health gate paused after Im Listening
  exited cleanly following dependency restarts. The checked recovery restored
  preparation. All four app network cutovers and drill functional checks have
  passed and were accepted by the owner. Its retained
  session, password/Google authentication, Spotify search/selection and Redis
  HTTP rate limit/Retry-After/recovery passed; temporary Spotify search errors
  recovered and are recorded in the runbook. Reusable controlled Chinwag,
  VitaTrack and Im Listening accounts were created with credentials outside Git;
  VitaTrack uses labelled synthetic records, and no Im Listening test post was
  published. The drill initially paused during finalization after Im Listening's
  clean-exit recovery failed to schedule a new task. The reviewed recovery build
  now waits for Swarm task removal before restoring one replica; the owner ran
  it successfully, retaining accepted checks and rollback inputs. Finalization
  and final isolation verification passed. At `drill/monitoring`, all ten
  services are 1/1, all five external monitors are Up, and Cloud PostgreSQL,
  Redis and Traefik charts and relevant Clear alert evaluations were checked.
  Repeated app/session flows passed with an external exception: Fleet's routing
  provider returns HTTP 403 quota errors, so road snapping is degraded while
  vehicles/SSE work. The owner proceeded with that issue recorded. The phase
  runner then completed the automatic rollback and baseline verification. At
  manual checkpoint 2/3 (`drill/restored-check`), original app networks, all ten
  services, retained sessions/data, reconnects, all five monitors and fresh Cloud
  collector samples/Clear alerts were checked. The owner accepted this baseline
  and final preparation began. It paused when the recovery wait included retained
  historical tasks as well as the completed task being replaced. The requested
  replica count was restored; all ten services are 1/1 and all four application
  HTTPS health endpoints return 200. The corrected helper waits for the specific
  completed task and no live tasks/containers, preserving the exact service spec.
  A disposable multi-task-history Swarm regression passes. Its guarded final
  preparation handoff retains the accepted drill/baseline, cursor, phase mode
  and both rollback inventories. The owner resumed it and reached manual
  checkpoint 3/3 (`accept`) after final automatic cutovers/install/finalization
  and verification. Final app/session/reconnect/protection checks passed,
  including controlled two-way Chinwag messages, retained VitaTrack reads,
  returning Google sign-in, Spotify and Redis rate-limit/Retry-After/recovery.
  Fleet live updates/reconnect/search passed; its provider quota still returns
  403 and road snapping remains an explicitly recorded exception. All ten
  services are 1/1 on the reviewed app networks, all five HTTPS health routes
  return 200, all five external monitors are Up, and fresh Cloud collector
  samples/relevant Clear alerts were checked. The owner accepted the result and
  the runner completed `finish`, recorded policies and cleared maintenance;
  original rollback/audit
  inputs are retained. Protected CI release/rollback and old-overlay retirement
  evidence remain required before task closure. The
  administrator helper offers a guided `run EXPECTED_FINGERPRINT` command with
  a rollback drill, saved progress and automatic final policy/snapshot handling.
  The reviewed phase-checkpoint update groups walkthroughs into the complete
  drill result, restored baseline and final acceptance, retaining all automatic
  checks and recording deferred checks honestly. It passed macOS/Linux race/vet
  and regression checks, was adopted through `resume-phases`, and retained the
  completed work and audit state. Production cutover acceptance is complete. The
  [dependency inventory, staged cutover, downtime, rollback and per-app
  verification record](../deploy/isolation/README.md) records completed drill
  checks and the remaining production evidence. The proposed Im Listening Redis overlay is named
  `im_listening_redis_clients`; it connects to the existing shared Redis service.

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
