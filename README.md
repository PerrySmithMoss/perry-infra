# Shared portfolio infrastructure

This repository manages Traefik, its restricted Docker API proxy, and PostgreSQL
for the existing single-node Docker Swarm. The live stack name is `perry-infra`.

Deploy with `ACME_EMAIL` set and the external networks/secrets already present:

```sh
docker stack config -c docker-compose.stack.yaml > /tmp/perry-infra.yaml
docker stack deploy --resolve-image always -c /tmp/perry-infra.yaml perry-infra
```

GitHub Actions requires `ACME_EMAIL`, `VPS_IP`, `DEPLOY_SSH_PRIVATE_KEY`, and verified
`VPS_KNOWN_HOSTS` secrets. It uses strict SSH host verification and pinned Actions.
The deploy key still has Docker-manager authority; replacing this with a host-owned
restricted deployment service requires administrator access and remains outstanding.

Traefik accesses Docker through a private, internal `docker_control` overlay. The
socket proxy permits service/task/network/node discovery; container access,
secrets/configs, and writes are denied. No Docker API port is published.

Existing applications retain `portfolio_network`. BulkBets instead uses external,
non-attachable encrypted overlays `bulkbets_ingress` (10.42.10.0/24; Traefik/API) and
`bulkbets_database` (10.42.11.0/24; PostgreSQL/API/migration jobs). Do not attach other
applications to these networks or publish database ports.

PostgreSQL 17.11 uses existing `postgres_user`/`postgres_password` secrets and the
existing `postgres_data` volume. It also requires `bulkbets_db_cert` and
`bulkbets_db_key` (key UID/GID 999, mode 0400). BulkBets clients use `bulkbets_db_ca`
and TLS `verify-full`. HBA rules require TLS for BulkBets roles and reject their
connections to other databases; legacy application rules are retained.

Docker configs are immutable. When changing `postgres/pg_hba.conf` after deployment,
version the config name in the stack before deploying. TLS secret rotations likewise
need versioned names. The current server certificate expires February 6, 2029.

Changes to this shared PostgreSQL service affect all portfolio apps. Use stop-first
updates, a temporary verified recovery copy, and check application availability.
Scheduled backups were explicitly declined by the owner for this portfolio.
