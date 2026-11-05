# Database TLS renewal

These files are the public CA and server certificates deployed on 4 November 2026.
Never add the CA or server private key. The weekly security workflow fails when
either certificate is within 90 days of expiry, or the server name/signature is
wrong. It checks this inventory, not a live connection to the private database;
update these files as part of every certificate rotation.

The current server certificate expires **6 February 2029 at 12:52:34 UTC**.
Renew before 8 November 2028 to keep the 90-day check passing. The CA signing key
is in the owner's restricted local recovery directory; it is not on GitHub.

1. Generate a new server key and CSR on the administrator's machine. Sign with
   the existing CA, retaining the SAN `DNS:perry-infra_postgres` and serverAuth
   extended key usage. Verify the signature, SAN, dates and matching public keys.
2. Create new versioned Swarm secrets for the certificate and key. Do not remove
   the currently mounted secrets. Change the external secret `name` mappings in
   `docker-compose.stack.yaml`, retaining the existing in-container target names
   and key owner UID 999/mode 0400.
3. Deploy during a short database maintenance window. The single database replica
   restarts. Verify the BulkBets API health, a `verify-full` connection and other
   shared applications. Keep the previous secret pair for rollback.
4. Commit the new public certificate here and rerun the certificate/image checks.
   Do not replace the CA during ordinary server-certificate renewal. A CA change
   requires a separate trust-overlap rollout to every database client.

An administrator can compare the inventory with the live service without reading
the private key:

```sh
container=$(docker ps -q --filter label=com.docker.swarm.service.name=perry-infra_postgres)
docker exec "$container" openssl x509 -in /run/secrets/bulkbets_db_cert -noout -fingerprint -sha256 -dates
openssl x509 -in postgres/tls/server.crt -noout -fingerprint -sha256 -dates
```

## PostgreSQL runtime candidate

The security workflow also builds and scans `postgres/Dockerfile` from the same
pinned stock base used by the stack. This candidate removes the unused gosu
helper from the runtime filesystem and runs as UID/GID 999. It is not the
deployed image. The smoke test initializes an isolated volume using the stock
image, then verifies the candidate can read and write it without a change in
server version, encoding, locale provider or collation version. This does not
replace a rehearsal against a one-off recovery copy or a health check of every
database consumer before a production change.
