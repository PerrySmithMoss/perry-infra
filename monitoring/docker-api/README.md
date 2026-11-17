# Restricted native Docker monitoring

This administrator-installed Go service replaces Netdata's membership in the
root-equivalent Docker group. It is separate from Traefik's existing HAProxy
socket proxy, its ACL and all Swarm networks. The native service has no TCP
listener and does not deploy or restart applications.

## Boundary

Systemd creates `/run/perry-netdata-docker-api.sock`, root:netdata 0660. A
separate dynamic service user has Docker-group access; Netdata does not. The
broker runs without capabilities, with no new privileges, a read-only system,
Unix-only sockets, bounded requests/timeouts and a 64 MiB memory limit. Its
privileged backend is fixed to `/var/run/docker.sock`.

Only these operations are accepted, with an optional Docker API version prefix:

| Operation | Returned fields |
| --- | --- |
| GET/HEAD `/_ping` | Readiness and API-version negotiation |
| GET `/version` | Version, API limits, OS and architecture |
| GET `/info` | Running, paused and stopped container counts |
| GET `/images/json` | Image IDs and sizes |
| GET `/containers/json` | IDs, names, images, state/status, creation time and the boolean Netdata ignore label |
| GET `/containers/ID/json` | Name and image only; ID must be 12 or 64 lowercase hexadecimal characters |

Container listing accepts only `all`, false `size`, and a single supported health
filter. Both Docker SDK filter encodings are normalized. Writes, request bodies,
encoded paths, unknown queries, logs, archives, events, exec, services, secrets and
configs are denied before contacting Docker. Caller headers/hosts are not
forwarded. Responses are reconstructed from allowlisted fields; environment,
mounts, commands, network details, arbitrary labels and error bodies are withheld.
The Cloud Docker live table consequently cannot display command/port metadata.

The explicit Docker health/state collector uses the new socket, with a
30-second initial-detection retry. `DOCKER_HOST` routes the actual cgroup-name
helper and its Docker CLI fallback through it too. The Netdata service also
masks the raw socket and refuses startup if the account regains Docker-group
membership, including after a vendor package update. Removing an account from a
group does not revoke existing processes' credentials: installation restarts
Netdata **and all three private socket relays**, then checks every Netdata-user
process for retained Docker access.

Docker-based service discovery is disabled. It currently creates a redundant
failing PostgreSQL job using stock connection settings and would require extra
container metadata. The configured private PostgreSQL, Redis, Traefik and
Prometheus jobs remain unchanged; host/cgroup collection and other discovery
pipelines remain available. Container names and images are retained, while
arbitrary Docker labels and Nomad environment-derived names are not exposed.

## Build, review and install

Use Go 1.27.3 and build static Linux/amd64 binaries outside the checkout:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go -C monitoring/docker-api build -trimpath -buildvcs=false -o /absolute/build/perry-netdata-docker-api .
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go -C monitoring/collectors build -trimpath -buildvcs=false -o /absolute/build/perry-metrics-admin .
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go -C deploy/infrastructure build -trimpath -buildvcs=false -o /absolute/build/perry-infra-release .
go -C deploy/infrastructure run . fingerprint ../..
```

All new runtime source/configuration files are included in the infrastructure
snapshot fingerprint and `deploy/install.sh`. Build and review from the same
checkout. Follow branch protection and passing `scan`; CI cannot install these
host files or refresh the administrator snapshot. The SSH forced command and
release-account sudo rights are unchanged.

Pause infrastructure releases and other administrator changes. Stage the
reviewed checkout/binaries outside the repository on the VPS. In the owner's
administrator terminal, use the trusted builder's expected old/new fingerprints:

```sh
sudo /absolute/build/perry-netdata-docker-api drill-and-install \
  /absolute/reviewed-checkout /absolute/build/perry-metrics-admin \
  /absolute/build/perry-infra-release OLD_FINGERPRINT NEW_FINGERPRINT
```

This is a first-install migration: it refuses existing target files, an existing
Docker-reader snapshot subtree, runtime drift or another maintenance operation.
Do not rerun it over an established installation or bypass a refusal. Subsequent
updates require a separately reviewed administrator migration.

The command first verifies baseline monitoring. It retains a private rollback
record, pauses the restricted infrastructure broker under its release lock, and
tests the new endpoint in a fresh Netdata-user process without supplementary
groups. It then installs the client configuration, removes only Netdata's Docker
membership and restarts monitoring processes. Existing application/runtime
configuration must match byte-for-byte. Only the new snapshot subtree and broker
binary are published; no Swarm deployment, SSH policy, key or credential changes
are performed by this migration.

`drill-and-install` performs a complete group-free verification, deliberately
restores the baseline and verifies that rollback, then repeats the cutover for
the final installation. Allow several minutes. Monitoring has brief collection
gaps during the restarts; application traffic continues. The original group
membership is temporarily restored during the controlled rollback drill.
Identity, metric history, database/application data and alert definitions are
preserved. A failed cutover automatically attempts the same scoped rollback.

## Verification and rollback

```sh
sudo /usr/local/libexec/perry-netdata-docker-api verify
id netdata
```

The privileged verifier checks socket permissions, installed binary/configuration
and snapshot fingerprints; group-free allowed reads and denials; direct raw
socket permission denial; all Netdata-user process groups; actual cgroup-name
resolution of all ten current containers; Docker health/state numeric samples;
the existing four private collectors, Cloud-only connectivity and public health;
and unchanged Traefik/proxy service specifications. Diagnostics remain in the
printed root-only rollback directory. Review live Cloud chart freshness/alerts
and repeat external endpoint/port checks before closing acceptance.

The rollback directory contains a dated record and old broker binary. It does
not contain or restore entire group databases, application manifests, SSH policy,
Cloud identity or database contents. For the exact directory printed by this run:

```sh
sudo /absolute/build/perry-netdata-docker-api rollback \
  /var/backups/perry-maintenance/docker-api.PRINTED_SUFFIX
```

Rollback restores the original Docker membership, removes only this migration's
managed files/subtree, restores the old broker, restarts monitoring and reruns
baseline private verification. It refuses changed snapshot/broker fingerprints
or another administrator's maintenance marker. Review any later host changes
before manual rollback; never restore an old archive over unrelated work.

## Tests

```sh
DOCKER_API_INTEGRATION=1 go -C monitoring/docker-api test -race ./...
go -C monitoring/docker-api vet ./...
```

The disposable integration test runs the actual pinned Netdata 2.12.3 Docker
collector and cgroup-name helper as an unprivileged user, sharing only the
filtered Unix socket. The test broker alone receives the local/CI Docker socket.
It verifies repeated state/health samples, names, secret-field projection and
negative API probes. Production is not used as a test fixture.

Production acceptance: [17 November record](../../docs/netdata-docker-access-20261117.md).
