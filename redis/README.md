# Shared Redis

`perry-infra_redis` owns the shared Redis lifecycle. The migration adopts the
existing external Docker volume **`im-listening_redis-data`** without copying it
to a new empty volume. Its historical name does not indicate current ownership.
Only one Redis process may mount it for writing. Keep the existing Redis 8.10.3
digest through cutover; upgrades are separate changes.

## Private clients and permissions

The administrator creates `redis_clients` as an **internal, encrypted,
non-attachable overlay**. Only approved client services and Redis join it.
There are no published Redis ports, Traefik routes, host mounts or connections
from the general `portfolio_network`. Clients use DB 0; database numbers do not
provide tenant isolation. Named ACL users restrict keys and commands instead.

| User | Key access | Purpose |
| --- | --- | --- |
| `im_listening` | `im-listening:*` | Sessions, Google flows, reset tokens, rate limits |
| `infra_probe` | `infra-probe:*` | Disposable second-application connectivity/isolation checks |
| `healthcheck` | None; PING only | Container health |
| `admin` | All | Administrator-only migration and verification |
| `default` | Disabled | No anonymous or legacy password access |

Im Listening mounts `im_listening_redis_url_v2` at `/run/secrets/redis_url`:
`redis://im_listening:<password>@perry-infra_redis:6379/0`. The application keeps
the existing session signing secret, cookie name, session keys and expirations.
The server mounts only the hashed ACL file and its PING credential. Admin/probe
URL secrets are mounted only into the temporary verification job, then detached
when it is removed. No application receives the administrator credential.

ACLs use an explicit command list, including the commands invoked by the actual
Node session store, rate limiting libraries, and Google authentication Lua.
Application users cannot enumerate keys, select another DB, administer Redis,
publish messages, flush data, or read/write another prefix. Lua is subject to
the same key permissions, including dynamically constructed keys.
See [Redis ACL documentation](https://redis.io/docs/latest/operate/oss_and_stack/management/security/acl/).

For another approved application, review its exact commands and key prefix,
add a separate hashed ACL user and versioned URL secret, update the reviewed
infrastructure snapshot, and attach only its service to `redis_clients`.
Give every application a unique prefix and a different credential. Run the
bidirectional denial tests before admitting its workload. The probe is a real
second-user/network test, not an additional production application deployment.

## Memory and persistence

Redis retains the existing **64 MiB maxmemory / 192 MiB container** allowance.
Before migration, production used about 1.8 MiB, with approximately 734 MiB host
RAM available. Those observations are a baseline, not a capacity guarantee.
AOF fsync runs every second, including during rewrites. A clean stop precedes
the volume backup and service handover. A host crash may lose roughly the last
second of acknowledged writes; AOF is not a backup. Keep root-only backups off
the public repository and copy them to approved private storage.

**`noeviction` is explicit.** A cache cannot evict a session to make room, but
any client can still exhaust shared capacity and cause writes to fail. Redis
ACLs do not impose per-user memory quotas. Require bounded keys, TTLs and a
reviewed capacity budget before adding workloads. Use another Redis instance
for an unbounded/evicting cache or stronger availability isolation. The 192 MiB
container allowance includes allocator overhead, buffers and AOF rewrite peaks;
check real peaks before increasing the data budget. Connections and buffers
are bounded, and optional RDB save scheduling is disabled in favor of AOF.

## Build and local verification

Use Go 1.27.3, matching the deployment brokers:

```sh
go -C redis test -race ./...
go -C redis vet ./...
sh redis/test.sh
GOTOOLCHAIN=local CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go -C redis build -trimpath -o /tmp/perry-redis-admin .
GOTOOLCHAIN=local CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go -C deploy/infrastructure build -trimpath -o /tmp/perry-infra-shared-redis-release .
```

`test.sh` creates disposable private Docker fixtures without publishing ports.
It uses the pinned, currently deployed Im Listening image to exercise the real
session store, Google completion/logout Lua, and both rate limiting paths under
the restricted ACL. It verifies a second client's private connection, cross-user
denials, server policy and retained session data after an AOF restart. On an ARM
builder, the amd64 application fixture uses emulation. Override
`REDIS_TEST_API_IMAGE` when reviewing a newer application image.

## Production cutover (administrator)

Completion requires live checks; a committed manifest is not proof of migration.
Coordinate the `im-listening/docker-stack.yaml` change with this snapshot.
Image-only application CI does not install networking or secrets. Do not deploy
the new app stack before the infrastructure is ready, and do not prune the old
service until verification passes.

Reserve a maintenance window: the API is stopped while Redis is backed up and
restarted, generally a few minutes. Pending requests may fail and an in-progress
OAuth flow may need retrying; unexpired sessions retain their contents and TTLs.
Avoid concurrent administrator changes and pause workflow dispatches during the
window. The tool takes the existing Im Listening release lock, and the existing
infra installer/broker retain their own exclusive locks and snapshot checks.

Transfer the reviewed checkout and both Linux binaries through administrator SSH.
Compare their checksums, keep an administrator session open, then run:

```sh
sudo /absolute/path/perry-redis-admin migrate \
  /absolute/path/perry-infra-checkout \
  /absolute/path/perry-infra-shared-redis-release
```

The Go tool refuses existing migration state, the wrong old image/data volume,
unexpected replicas, another node, existing proposed credentials, or an already
present shared service. It does the following without printing credentials:

1. Saves both original service specifications, the installed infrastructure
   snapshot/binary/SSH/sudo configuration and the app release policy in root-only
   `/var/lib/perry-redis-migration`. It creates distinct random 256-bit passwords,
   hashed ACL inputs and external Swarm secrets there.
2. Creates the private overlay, stops the API, records hashes of existing keys,
   values and absolute expiration times, then cleanly stops the old Redis.
3. Archives the stopped data volume through a read-only, networkless container
   and checks the archive. It never starts both Redis servers simultaneously.
4. Reuses the existing infra release public key to run `deploy/install.sh`,
   installing the reviewed broker and snapshot, including `redis/redis.conf`.
   The installed broker verifies its fingerprint and deploys `perry-infra`.
5. Confirms every unexpired key has identical content and absolute expiry,
   changes only the API's Redis URL secret/network, and starts it. This preserves
   its current image, OAuth/Spotify configuration, database and signing secrets.
6. Runs a temporary verification service on `redis_clients` with the deployed
   app image, confirms public API health, removes the probe job/config, and
   records the changed API spec with the existing portfolio release broker.

If interrupted while initially stopping the API/old Redis, before the data backup or
snapshot installation, `redis-admin resume CHECKOUT BROKER_BINARY` can continue
using the saved credentials and installation backup. It verifies both service
identities/specifications, allowing a zero/one replica pause and a forced-restart
counter change. Other changes
or a later migration phase are refused. An earlier preservation proof is retained
and refreshed after the resumed API stop. Scaling submits Docker's change detached
and checks container health/stoppage directly, avoiding its zero-replica wait hang.

On other failures, inspect privately and use rollback below. Do not rerun migration
over existing state or remove its data to bypass a failed preflight. Secret
inputs and all backups remain in the private state directory for recovery.

Before removing the old service, verify all of these in production:

- A browser already signed in before cutover remains signed in after reload.
- Controlled-account password sign-in, Google sign-in, session reload and logout
  work. Do not paste passwords, cookies or reset tokens into chat or Git.
- The Go tool's private verification reports passing session, Lua, rate limiter
  and bidirectional ACL tests. Confirm the API returns normal rate limit headers;
  test HTTP 429 using the controlled account/IP without flooding other users.
- The API and Redis have one healthy task each; Redis has no published port and
  its network remains internal/encrypted/non-attachable. Other public APIs are
  healthy. Verify the restricted Im Listening image release against its new policy
  and that an uninstalled infra fingerprint is still rejected.

Repeat the private checks without another cutover using
`sudo /absolute/path/perry-redis-admin verify /absolute/path/perry-infra-checkout`.
Record results and downtime in `docs/tasks.md`. Only after they pass:

```sh
docker service inspect im-listening_redis --format '{{.Spec.Mode.Replicated.Replicas}}'
# Must be zero; only then remove the retained old service.
docker service rm im-listening_redis
```

Retain `im-listening_sessions`, the old versioned secrets, the private state and
the original service specs through the rollback window. Never remove the external
data volume. The app manifest has no Redis service or volume declaration, so
later app deployments cannot own its lifecycle.

## Rollback

The 2026-11-11 migration's old-stack rollback window is now closed; see
[production cleanup](#production-cleanup-2026-11-11). The procedure below applies
only while the original migration resources still exist. The helper checks saved
network/secret/config IDs before stopping either live service and refuses rollback
when any dependency has been retired.

Pause releases and use the administrator connection:

```sh
sudo /absolute/path/perry-redis-admin rollback
```

Rollback locks application releases, stops the API and shared Redis completely,
restores (or recreates) the original Redis service from its saved spec, and starts
it on the **current** data volume. It restores the exact original API spec and
the saved infrastructure installation/app policy, validates sudo/SSH config and
reloads SSH. Retaining the current volume preserves writes made after cutover.
The old overlay and secret resources must still exist. Keep the new Redis at
zero replicas; a restored old infra snapshot does not prune it automatically.
Verify the same API/browser checks and restricted releases again.

The data archive is disaster recovery only. If the current data is unusable,
stop both servers and the API before an administrator restores that archive into
an empty recovery volume. Restoring the cutover backup loses later writes; do
not extract it over live Redis data or silently discard newer sessions. Migration
does not change the AOF version or application key format.

## Production verification, 2026-11-11

The administrator completed the cutover with the corrected `redis-admin resume`
helper. An initial attached Docker zero-replica wait stalled after stopping the
API; service availability was restored before retrying. The resumed cutover
created shared Redis at **22:47:48 UTC**; the API was healthy at **22:49:30 UTC**.
The initial interruption was additional downtime, not part of that final interval.
The helper verified every unexpired key's content and absolute expiration time.

Live acceptance checks passed:

- The existing Chrome session remained signed in as the same user after reload.
- The deployed application's Redis libraries passed session storage, Google
  completion/logout Lua, transaction/Lua rate limiting, and bidirectional key/command
  isolation with a second named client on the private network.
- `verify-api.mjs` exercised the live HTTP handlers with a random unpublished
  account: registration, password login and rejection, session reload and logout
  revocation. A reserved forwarded client address reached HTTP 429 with rate limit
  and Retry-After headers. The account/profile, test sessions and rate keys were
  removed afterward. Credentials and cookies stayed in process memory. This checks
  the production API handlers; it does not perform a new external Google OAuth flow.
- Redis and the API each have one healthy task. Redis retains the original volume,
  64 MiB/noeviction data policy and 192 MiB container allowance. Its encrypted overlay
  is internal and non-attachable; no Redis port is published or listening on the host,
  and TCP 6379 was unreachable from the external verification machine.
- The restricted Im Listening release command passed for its running digest and
  revision against the newly recorded policy. The installed infrastructure
  fingerprint matches this checkout. All five public API health endpoints returned 200.

After verification, `im-listening_redis` was removed. The original overlay and old
secrets were retained until the owner retired the migration rollback window below.
The current data volume, saved specs and root-only installation/data backups remain.
The state file in the private migration directory
records the last automated checkpoint (`awaiting-browser-verification`); this
runbook and `docs/tasks.md` record the subsequent manual verification and removal.

To repeat the HTTP check from the reviewed checkout through administrator SSH:

```sh
ssh pez-vps 'api=$(docker ps -q --filter label=com.docker.swarm.service.name=im-listening_api); test -n "$api"; docker exec -i "$api" node --input-type=module' < redis/verify-api.mjs
```

This creates and removes a temporary account and about 120 cheap requests from
an isolated reserved test address; run it deliberately, not as a recurring health
probe. `redis-admin verify CHECKOUT` separately repeats the private ACL/client checks.

## Production cleanup, 2026-11-11

After both PRs merged, CI/deployments passed and the owner explicitly ended the
migration rollback window, the following unused resources were removed:

- Overlay `im-listening_sessions`.
- Secrets `im_listening_redis_url_v1` and `im_listening_redis_config_v1`.
- 25 stopped Swarm containers and 67 old cached images across the VPS.
- Duplicate migration binaries/source archive and 51 macOS archive metadata files.
- Three obsolete deployment staging bundles containing pre-shared-Redis manifests,
  old installers and Python brokers. Checksums/public-key audit records were
  preserved separately; installed root-owned broker/snapshot paths were retained.

The service audit checked current and previous specifications before removing the
old network/secrets. Cache cleanup retained every current/previous service image
and both Im Listening images in `API_ROLLBACK_ALLOWLIST`; it used no forced image
removals. The VPS now has 9 running containers and 16 retained images. Docker image
storage fell from 19.04 GB to 4.87 GB. Ordinary image release rollback remains
available; restoring the former app-owned Redis stack is retired.

Keep **`im-listening_redis-data`**: it is the live shared Redis volume. Current
Redis config/ACL/healthcheck secrets, application URL, admin/probe credentials,
administrator tooling and the root-only credential/backup directory are still
required or deliberately retained. No active stack, database or current credential
was deleted. All five public API health endpoints passed after cleanup.

Two unreferenced historical volumes, `vita-track_postgres_data` and
`vita-track_letsencrypt`, remain pending a data-retention review. They contain
persistent data and were not treated as disposable cache. Other unmounted secrets
used by administration, migrations or verification were retained as well.

## Rotate a single credential

Keep the current private ACL input and that user's current URL in administrator
storage. The Go tool preserves all other users and permissions:

```sh
sudo /absolute/path/perry-redis-admin rotate im_listening \
  /private/current/users.acl /private/current/im-listening-url /private/rotation
```

The **new** directory contains `users.acl` with old and new password hashes and
`url` with the new password. Import them into new versioned Swarm secrets
(for example `redis_users_v2` and `im_listening_redis_url_v3`) using file/stdin
input, update both reviewed manifests, and install the infra snapshot first.
Deploy Redis stop-first with both hashes, then redeploy the API with the new URL,
keeping mount targets unchanged and recording its updated release policy.
Verify new authentication and the browser/session checks. To retire the old hash:

```sh
sudo /absolute/path/perry-redis-admin retire im_listening \
  /private/rotation/users.acl /private/rotation/url /private/retired
```

Create another versioned ACL secret from `/private/retired/users.acl`, install
the reviewed infra snapshot, and redeploy Redis. Confirm the retired password
fails and the new one succeeds before deleting unreferenced old secrets. Test
through stdin/private files; never put passwords or Redis URLs in argv. Keep
the current ACL and URLs in private storage. The same Go commands support
`infra_probe` and `admin`; healthcheck rotation requires a matching new hash
and password secret installed together during the Redis maintenance window.

## Monitoring ownership

Netdata host/container resource monitoring is separate. Private Redis internals
collection remains the next task in `docs/tasks.md`: add a separate collector
user with just its required commands and a reviewed private connection path.
Do not reuse any application/admin credential, publish 6379, or attach Redis
to the general application network for that follow-up.
