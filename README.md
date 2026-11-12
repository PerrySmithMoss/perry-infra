# Shared portfolio infrastructure

This repository manages Traefik, its restricted HAProxy Docker API proxy, PostgreSQL and shared Redis
for the existing single-node Docker Swarm. The live stack name is `perry-infra`.

For owner-controlled recovery only, with `ACME_EMAIL` set and external networks
and secrets already present:

```sh
docker stack config -c docker-compose.stack.yaml > /tmp/perry-infra.yaml
docker stack deploy --resolve-image always -c /tmp/perry-infra.yaml perry-infra
```

GitHub Actions deploys administrator-installed snapshots through the dedicated
`infra-release` SSH account. It requires `VPS_IP`, verified `VPS_KNOWN_HOSTS`
and the `production` environment secret `INFRA_RELEASE_KEY`. See the
[installation and deployment guide](deploy/README.md).

Traefik accesses Docker through a private, internal `docker_control` overlay. The
socket proxy permits service/task/network/node discovery; container access,
secrets/configs, and writes are denied. No Docker API port is published.

The installed snapshot isolates each application on its own ingress and,
where needed, private database/Redis overlays. Production cutover and the full
specification rollback drill passed on 2026-11-12. The
[isolation and rollback runbook](deploy/isolation/README.md) coordinates all four
application manifests, administrator-installed snapshots and image-only release
policies. Protected CI release/rollback checks are in progress; old overlays stay
retained until the owner ends the network rollback window.

BulkBets continues to use external,
non-attachable encrypted overlays `bulkbets_ingress` (10.42.10.0/24; Traefik/API) and
`bulkbets_database` (10.42.11.0/24; PostgreSQL/API/migration jobs). Do not attach other
applications to these networks or publish database ports.

PostgreSQL 17.11 uses `postgres_user` and the versioned
`postgres_password_v2` secret, mounted at `/run/secrets/postgres_password`,
with the existing `postgres_data` volume. Rotating the mounted secret does
not change a password on an initialized database. Rotate the role in
PostgreSQL and verify both the new and retired credentials over the network.
Chinwag and VitaTrack use separate PostgreSQL roles that own their Prisma
tables and have access to their respective application databases. The
one-time ownership change is in `postgres/legacy-app-role.sql`. It also requires `bulkbets_db_cert` and
`bulkbets_db_key` (key UID/GID 999, mode 0400). BulkBets clients use `bulkbets_db_ca`
and TLS `verify-full`. HBA rules require TLS for BulkBets and Im Listening roles.
Chinwag, VitaTrack and Im Listening roles are restricted to their own database
and private source subnet, before the retained administrator rules.

Docker configs are immutable. When changing `postgres/pg_hba.conf` after deployment,
version the config name in the stack before deploying. TLS secret rotations likewise
need versioned names. The current server certificate expires February 6, 2029.

Changes to this shared PostgreSQL service affect all portfolio apps. Use stop-first
updates and check application availability after deployment. The owner accepts a
fresh database if the existing data becomes unusable and authorized this rollout
without a backup.

The reverse proxies are Traefik 3.7.17 and HAProxy 3.4.7-alpine, pinned by digest.
`docker-proxy/test.sh` verifies allowed discovery and denied container/credential/
write requests before deployments. Weekly/manual image scans retain full reports.
PostgreSQL runs directly as UID 999 with a read-only root filesystem, bounded tmpfs
mounts, and a 640 MiB memory limit. Its existing data volume must belong to UID 999.
The pinned runtime image removes the unused gosu helper. Unfixed Debian library
findings remain in the scan artifact. The PostgreSQL gate fails on fixable high
or critical findings.
See BulkBets's `docs/security-remediation-2026-11-04.md` for reachability caveats and
remaining administrator/plan access requirements.

## Monitoring

[Shared Redis operations](redis/README.md) covers the Im Listening data/session
migration, private encrypted client overlay, per-application ACLs, memory and
persistence policy, credential rotation and rollback. Redis adopts the existing
`im-listening_redis-data` volume and publishes no port. The production cutover
must precede deploying the application's shared-Redis manifest.

[Netdata Community host monitoring](monitoring/README.md) is managed separately
from Swarm releases: Cloud-only dashboard access, administrator installation,
private claim-token entry, safety checks, personal email alert setup, and an
external API-check inventory. The infra-release CI account gains no host access.

[Private service collectors](monitoring/collectors/README.md) add PostgreSQL,
Redis and Traefik metrics through administrator-managed loopback/Unix socket
relays and separate encrypted overlays. Install the matching snapshot before
deploying this manifest; it requires the new versioned Redis ACL secret and host
socket directory. Application data and administrator credentials are retained.
