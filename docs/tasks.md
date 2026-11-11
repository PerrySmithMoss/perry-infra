# Infrastructure tasks

Follow-ups for Netdata Community with Cloud-only access. Track work here even
when implementation belongs in an application repository. Completion requires
verification in production; configuration alone is not evidence of coverage.

- [ ] **Add a public health endpoint for Chinwag and external monitoring.**
  Implement a cheap, unauthenticated endpoint in `PerrySmithMoss/chinwag` that
  returns 200 when the API is ready and a non-success response when unavailable,
  without exposing internal diagnostics. Support GET and HEAD so UptimeRobot
  Free can monitor it. The existing `/api/users/me` probe returns 403 without
  credentials and is unsuitable for the default external HTTP check. Keep
  authentication on existing routes. Update `monitoring/endpoints.tsv`, create
  the five-minute monitor with owner email alerts, and verify it reports Up.

- [ ] **Add a public health endpoint for Vita Track and external monitoring.**
  Implement a cheap, unauthenticated GET/HEAD endpoint in
  `PerrySmithMoss/VitaTrack`. Return 200 when ready, a non-success response on
  failure, and no sensitive diagnostics. The existing Docker healthcheck uses a
  GraphQL POST with JSON headers; headerless GET is rejected by CSRF protection.
  Preserve that protection. Update `monitoring/endpoints.tsv`, create the
  five-minute monitor with owner email alerts, and verify it reports Up.

- [ ] **Configure private PostgreSQL metrics collection in Netdata.**
  Design a private connection path from the host agent to the Swarm service;
  PostgreSQL currently has no published port. Use a dedicated non-superuser
  monitoring role with only required permissions and appropriate TLS. Store
  credentials outside Git. Keep application network isolation and the shared
  database administrator credentials intact. Verify connection, transaction,
  and lock/deadlock metrics and relevant alerts without disrupting applications.
  Document configuration, credential rotation, and rollback.

- [ ] **Configure private Redis metrics collection in Netdata.**
  Coordinate with the `PerrySmithMoss/im-listening` infrastructure because Redis
  belongs to that stack. Provide a private collector path and a dedicated ACL
  with only commands required by the collector. Keep Redis off public ports and
  credentials out of Git. Verify memory, evictions, connections, and hit/miss
  metrics, then document configuration, credential rotation, and rollback.

- [ ] **Configure private Traefik request metrics in Netdata.**
  Enable a dedicated private metrics entrypoint and a collector connection path
  through an administrator-reviewed infrastructure change. Do not expose the
  Traefik API/dashboard or metrics publicly, broaden the Docker socket proxy
  ACL, or bypass the existing snapshot approval boundary. Verify request counts,
  latency, and 5xx metrics per service where supported. Document alert settings,
  deployment verification, and rollback.

- [ ] **Verify end-to-end monitoring email delivery.**
  Confirm receipt of the UptimeRobot test email already requested; resend a test
  if necessary after checking the owner alert contact and spam folder. Once the
  VPS agent is installed and claimed, stop only Netdata briefly, verify the
  Cloud Unreachable email, restart it even if email fails, and verify recovery.
  Do not stop production applications. Confirm Critical, Warning, Clear,
  Reachable, and Unreachable email selections remain enabled on Community after
  any Business trial. Record the test date and result without publishing private
  mailbox content or credentials. Receipt and recovery verification are required
  before marking this task complete.
