# Netdata Docker access hardening — 17 November 2026

## Baseline review

The native Netdata 2.12.3 agent, go.d plugin and three private socket relays
inherited UID/GID 987 and supplementary Docker GID 994. The raw Docker socket was
root:docker 0660 without an extra ACL. Netdata Cloud showed a running stock
`docker/local` job targeting `unix:///var/run/docker.sock` every ten seconds,
with fresh container state/health series and ten running containers. Docker-based
service discovery also used the raw socket and created a redundant failing
PostgreSQL job; the explicitly configured private PostgreSQL job was running.

The owner's read-only administrator test at **13:15:29 UTC** confirmed that a
fresh Netdata process could reach Docker with its normal groups, while a fresh
process with supplementary groups cleared had neither socket write permission
nor a working Docker connection. A final normal-group request still succeeded.
That review changed no account, collector or service. Its private evidence is
`/var/backups/perry-maintenance/docker-access-20261117.OBeTJ7Xw`.

The owner then authorized a separate restricted, read-only API path and removal
of Netdata's Docker membership while preserving monitoring and Traefik's existing
proxy restrictions. The migration and rollback procedures are in the
[Docker-reader runbook](../monitoring/docker-api/README.md).

## Acceptance

- [x] Implement a separate Unix-only, allowlisted and response-filtering broker.
- [x] Test actual unprivileged Netdata Docker health/state collection and cgroup names, plus API/metadata denials, using disposable local containers.
- [ ] Pass the protected PR scan and install the reviewed host/snapshot configuration through the administrator path.
- [ ] Pass a controlled production monitoring rollback drill and the final group-free cutover.
- [ ] Verify actual process groups/raw-socket denial, all collectors and Cloud freshness, public endpoints/private ports and unchanged Traefik/proxy specifications.

Production hardening is pending. Configuration/tests in Git do not establish
that Docker-group access has been revoked on the VPS.
