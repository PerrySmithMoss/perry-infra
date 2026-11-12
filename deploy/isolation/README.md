# Application network isolation

This is the coordinated implementation for the open isolation task in
[docs/tasks.md](../../docs/tasks.md). **The final isolated topology and combined
application/monitoring checks have been verified and accepted. The runner
completed `finish` successfully.** The rollback drill and restored baseline
were accepted. All ten services are 1/1, all five public health URLs return 200,
all five external monitors are Up, and Cloud collection and relevant Clear
alerts were checked. Each app now uses only its reviewed ingress and applicable
database/Redis overlays; no app remains on `portfolio_network` or the old shared
`redis_clients`. Fleet's
previously diagnosed routing-provider quota issue remains open; road snapping
is degraded while live vehicles/SSE work. The owner started the guided run on
2026-11-12 and proceeded with that issue recorded. Original service
specifications and installation are retained. Final acceptance, release-policy
recording and maintenance removal completed. Protected CI release/rollback
evidence and old-overlay retirement remain separate requirements for task
closure.

### Final-preparation recovery (completed)

The owner stopped the failed `final/prepare` prompt and continued with:

```sh
ssh -tt -o ConnectTimeout=10 -o IPQoS=none \
  -o ServerAliveInterval=15 -o ServerAliveCountMax=3 pez-vps \
  'sudo -p "VPS sudo password: " /home/pez/perry-network-isolation-20261112-task-recovery.xmYblp/perry-network-admin resume-preparation b299443ef76b27a11c99964b3aede0322a64e96c5bfb7add8416de2747cf51d5'
```

The failed wait included the historical completed task from the earlier drill.
Swarm removed the task being recovered and scheduled a healthy replacement when
the helper restored one replica, but retained the older history row. The new
wait records the specific completed task before scaling down and requires that
task to disappear, with no desired-running tasks or running containers, before
restoring one replica. It still requires the exact zero-replica service spec,
uses a fresh service version, and verifies the healthy replacement and unchanged
final spec. Task history is neither deleted nor required to be empty.

This separately staged bundle preserves the immutable phase runner. Adoption
accepts only a pending preparation before any application cutover, with the same
reviewed fingerprint and verified previous bundle. For final preparation it
also requires the accepted restored-baseline and archive events, the retained
successful rollback drill, and identical original service inventories in both
passes. It archives the previous progress under a unique filename, verifies the
complete preparation topology and public health, and records the new binding
without advancing the cursor or changing phase mode. It does not restart the
already prepared shared services. The existing drill audit and final rollback
inputs remain intact. Rerun the same command after a later pause. Only the final
combined `accept` checkpoint remains.

Regression checks cover historical task retention, missing task IDs, diagnostic
failures, concurrent service drift, scale-down timeout replica restoration,
guarded final-preparation adoption, preserved cursor/mode/evidence, and retained
drill inventory validation. A disposable local Swarm test creates a historical
task, cleanly completes its replacement, then proves recovery creates a new
running task with the exact original service specification while history stays.
The host Swarm and production are not changed by this fixture. The owner adopted
the recovery, completed the final automatic cutovers/install/finalize/verification,
and reached the final combined acceptance checkpoint. The later evidence below
records its functional and monitoring checks; acceptance is still pending.

The recovery passed macOS/Linux race and vet checks with Go 1.27.3. Its Linux
amd64 helper SHA256 is
`8e2f9d6fb51047dc59efe5eae29082f44926d30a3f93ccc5e967bd271b47949f`.

## Guided administrator cutover

Use the single-command `run EXPECTED_FINGERPRINT` flow from the staged package
listed under [Required production evidence](#required-production-evidence).
The expected fingerprint is copied from the trusted builder's reviewed checkout;
the runner never accepts a digest merely because the VPS reports it.

The command verifies the bundle checksums and reviewed runtime fingerprint,
holds all four application release locks, retains the original specifications
and installation, prepares the shared services, and migrates Fleet, Chinwag,
VitaTrack and Im Listening in order. Every app still gets its automatic health,
database/migration, Redis and network-denial checks before the next app changes.
It installs/finalizes the snapshot and runs the full topology, PostgreSQL, Redis
and Netdata checks. It then performs the full specification rollback drill,
verifies the restored baseline, retains that audit set, and repeats the final
cutover. Brief shared-service interruptions occur during both passes and the
drill; Swarm convergence still takes time even with fewer manual interruptions.

New runs use **three manual checkpoints**, with automatic work between them:

1. `drill/monitoring`: all applications and monitoring after the complete drill
   cutover and its last shared-service restart.
2. `drill/restored-check`: existing sessions, core app reads/reconnect and
   monitoring on the automatically verified restored baseline.
3. `accept`: all applications and monitoring after the complete final rollout,
   before recording release policies and clearing maintenance.

Individual walkthroughs are explicitly recorded as deferred to their phase's
combined check, never as passed. Reuse the controlled accounts and existing test
records; repeated signup, account linking and redundant provider traffic are
unnecessary. The final monitoring and acceptance prompts are combined. Automatic
failures still stop immediately, and final acceptance is still required before
release policies or maintenance change. Existing runs retain their recorded
validation mode until the explicit `resume-phases` handoff below.

The earlier handoff from `drill/monitoring` (also supported at the restored-baseline
prompt) used the separately staged phase runner after stopping the old runner:

```sh
ssh -tt -o ConnectTimeout=10 -o IPQoS=none \
  -o ServerAliveInterval=15 -o ServerAliveCountMax=3 pez-vps \
  'sudo -p "VPS sudo password: " /home/pez/perry-network-isolation-20261112-phases.R0L2Hx/perry-network-admin resume-phases b299443ef76b27a11c99964b3aede0322a64e96c5bfb7add8416de2747cf51d5'
```

The handoff validates both bundle bindings and the same reviewed fingerprint,
then checks either the completed isolated topology or the restored baseline.
It archives the original progress, keeps its exact cursor, accepted checks and
rollback inputs, and records the phase-checkpoint mode. It does not repeat a
completed deployment or silently approve the current prompt. From the current
`drill/monitoring` prompt three manual gates remain; from the restored-baseline
prompt two remain. Rerun this same command after a later pause. The running old
bundle has not been modified; the staged Linux amd64 helper SHA256 is
`5a0494b782dda3ba57ef40f129947d346a2a57837fbe5b7d87dad4e62e25f966`.

The phase workflow passed race/vet checks on macOS and Linux. Regression tests
compare its complete automatic action sequence with the original flow, require
the three real checkpoints, prove failed app checks stop the next app, and prove
EOF at final acceptance cannot record policies or finish. Handoff tests retain
the cursor/prior evidence and reject unsafe steps, drifted fingerprints,
unsupported modes and rolled-back/unsupported progress. The owner subsequently
adopted this bundle and reached manual checkpoint 2/3 after the automatic
rollback and baseline verification passed.

At each prompt, type the displayed `pass STEP_NAME` only after its checks pass.
Type `rollback` to restore the retained specifications, or `stop` to pause.
Blank input, EOF, a disconnected terminal and stale responses never approve a
check. Run through `ssh -t`; confirmations are read from `/dev/tty`, not piped
stdin. Ctrl-C/SIGHUP requests a pause at the next stage boundary; allow an
in-flight administrator subprocess to settle before the release locks close.
The same command resumes a stopped prompt without repeating completed cutovers.
An interrupted mutation reruns its live consistency checks; an incomplete
preparation or unexpected service change requires inspection/recovery.

The updated runner reports shared-service updates and waiting tasks. After
shared-service restarts it checks the real application health routes and
recovers a single-replica client only when its latest task explicitly completed
with exit code zero. It temporarily scales that client to zero and restores its
exact specification to one replica, using a fresh service version. It does not
change restart policies, images, credentials or data, and rejects unrelated
specification drift. These checks also run after finalization and rollback.

After final acceptance, the runner validates the full live specs against their
retained network-only changes, writes the existing image-release policy format,
and removes the infrastructure maintenance marker under its release lock.
Policies remain unchanged until that acceptance. CI permissions and forced SSH
commands are unchanged. Keep merges/dispatches coordinated during maintenance;
app release attempts fail while locks are held, and infrastructure deployments
are blocked by the marker.

Root-only progress, timestamps and subprocess logs are retained under
`/var/lib/perry-network-cutover`; the successful drill's original audit set is
`/var/lib/perry-network-cutover/rollback-drill`. The final rollback inputs remain
under `/var/lib/perry-network-isolation`. A rollback requested at a prompt retains
both progress and recovery inputs and ends the run; preserve/review that attempt
before starting another one. Existing manual migration state is never adopted
or overwritten. The bundle directory, manifest and fingerprint must match when
resuming.

Successful guided completion is production cutover acceptance, not closure of
the task: actual protected CI image/infrastructure releases and image rollbacks
still need evidence, and old overlay retirement waits for the owner to end the
rollback window. Neither overlay is automatically deleted. The detailed manual
commands below remain available for administrator inspection and recovery.

## Reviewed dependencies and topology

| Application | Production service | Ingress / subnet | PostgreSQL overlay / subnet; role; database | Redis |
| --- | --- | --- | --- | --- |
| Fleet | `fleet_fleet-api` | `fleet_ingress` / `10.42.32.0/24` | None: vehicle state is in process memory | None |
| Chinwag | `chinwag_chinwag-api` | `chinwag_ingress` / `10.42.30.0/24` | `chinwag_database` / `10.42.31.0/24`; `chinwag_app`; `chinwag` | None |
| VitaTrack | `vita-track_vita-track-api` | `vita_track_ingress` / `10.42.36.0/24` | `vita_track_database` / `10.42.37.0/24`; `vita_track_app`; `vita_track` | None |
| Im Listening | `im-listening_api` | `im_listening_ingress` / `10.42.34.0/24` | `im_listening_database` / `10.42.35.0/24`; `im_listening`; `im_listening` | `im_listening_redis_clients` / `10.42.38.0/24`, to shared `perry-infra_redis` |

All eight overlays are encrypted and non-attachable. Ingress overlays allow
outbound Internet access, needed by Cloudinary (Chinwag), Google/USDA
(VitaTrack), OpenRouteService (Fleet), and Google/Spotify/SMTP (Im Listening).
Only the app and Traefik join each ingress overlay. Database and Redis overlays
are internal. Each database overlay has only PostgreSQL and its app; explicitly
reviewed, temporary migration jobs may join that app's database overlay only.
The Redis client overlay has only Im Listening and the shared Redis service.
It is a network, not the retired `im-listening_redis` service. No Redis server,
data volume, secret or ACL user is recreated or rotated.

The inventory used live service networks, route labels, mounted secret names,
and connection URLs with passwords omitted, plus application source and startup
scripts. All database roles lack superuser, CREATEDB, CREATEROLE, replication and
BYPASSRLS privileges. Chinwag, VitaTrack and Im Listening run Prisma migrations
before starting the API. Their existing `postgres` hostname continues to resolve
through the alias on each private database overlay. Im Listening already verifies
the PostgreSQL CA; that TLS requirement remains. Chinwag and VitaTrack retain
their existing PostgreSQL transport inside encrypted overlays. HBA now restricts
all three roles to their own database and source subnet, before the retained
administrator catch-all. BulkBets's TLS rules and roles are retained.

BulkBets keeps `bulkbets_ingress` and `bulkbets_database`. The three metrics
overlays, host-only relay, and Docker control overlay remain separate. Traefik's
metrics listener also uses its existing monitoring-CIDR HTTP allowlist: an ingress
peer may complete a TCP handshake on port 8082, but must receive HTTP 403 from
`/metrics`. Database, Redis and Docker API services publish no ports.

Swarm network membership limits direct connectivity; the public HTTPS APIs remain
public. Shared Traefik, PostgreSQL and Redis remain trust/availability boundaries.
An approved migration job receives only its own database credential and network,
and is removed after checking its exit status. Do not attach any other app to a
Redis client's overlay; provision a separate overlay and named ACL for each
future client, retaining the prefix/command restrictions and noeviction policy.

## Review, build and coordinate repositories

The matching `docker-stack.yaml` changes are in these application checkouts:
`chinwag`, `logistics-realtime-dashboard`, `VitaTrack`, and `im-listening`.
They replace all old attachments and select `traefik.swarm.network` explicitly.
Chinwag/VitaTrack also remove the invalid cross-stack `depends_on: postgres`.
Im Listening's standalone migration-job example now uses only its database
network. No application image or database schema change is required.

Review and land these changes together. Pause production workflow dispatches
and merges during the migration; image-only CI cannot apply networking. Do not
redeploy full legacy app stacks: their manifests omit some administrator-reviewed
live settings/secrets. This helper changes the live service's networks and one
Traefik label while retaining the rest of its specification. Keep all current
and previous images cached throughout the rollback window.

From the infrastructure checkout, with Go 1.27.3:

```sh
go -C deploy/infrastructure test -race ./...
go -C deploy/infrastructure vet ./...
ISOLATION_DOCKER_TEST=1 go -C deploy/isolation test -race ./...
go -C deploy/isolation vet ./...
go -C redis test -race ./...
go -C redis vet ./...
GOTOOLCHAIN=local CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go -C deploy/infrastructure build -trimpath -o /tmp/perry-infra-isolation-release .
GOTOOLCHAIN=local CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go -C deploy/isolation build -trimpath -o /tmp/perry-network-admin .
GOTOOLCHAIN=local CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go -C redis build -trimpath -o /tmp/perry-isolation-redis-admin .
GOTOOLCHAIN=local CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go -C monitoring/collectors build -trimpath -o /tmp/perry-isolation-metrics-admin .
shasum -a 256 /tmp/perry-infra-isolation-release /tmp/perry-network-admin /tmp/perry-isolation-redis-admin /tmp/perry-isolation-metrics-admin
```

The disposable Docker test uses isolated bridge networks without published ports,
real PostgreSQL HBA/TLS authentication, and the deployed Im Listening image's
actual pg/Redis libraries. It checks each database role's own migrations,
cross-database rejection, wrong-source rejection, required Im Listening TLS,
Redis ACL denials and direct-IP connectivity denials with positive controls.
Production Swarm overlays and account flows still require the checks below.

Transfer the reviewed checkout and Linux binaries through the administrator SSH
path. Compare SHA-256 checksums on the VPS. Keep a separate administrator session
open. Never give a release account Docker access or use Docker host mounts to
obtain sudo. The helper refuses non-root execution.
For guided mode, the bundle must contain the four named Linux binaries, the
reviewed `checkout`, and `source.tar.gz`. Its `SHA256SUMS` must cover every binary,
the source archive and `checkout/deploy/install.sh`; coordinated app manifests may
also be included. The staged bundle below already supplies that manifest. The
runner rejects missing/tampered inputs and symlink/traversal entries before any
live mutation, then independently checks the supplied runtime fingerprint.

## Maintenance and staged cutover

Reserve roughly 10–20 minutes plus the time to check application flows. Adding
interfaces replaces the shared PostgreSQL/Redis tasks with stop-first updates;
expect short outages across their clients and a brief Traefik interruption.
Application network changes replace each API task, with a possible short 5xx
window; Fleet's process-local vehicle state and SSE connections can restart.
Public HTTPS readiness checks retry failed requests and non-200 responses every
five seconds for up to two minutes across all five routes, allowing application
startup and Traefik's polling to converge. Each request retains its 15-second
timeout and normal TLS verification; persistent failures still stop the rollout.
Pending requests/OAuth callbacks may need retrying. PostgreSQL volumes, Redis
AOF, session signing secrets, unexpired sessions and all credentials are retained.
The helper performs no schema migration, data restore, volume removal or cleanup.

1. As administrator, run the reviewed helper and broker, using absolute paths:

   ```sh
   sudo /absolute/path/perry-network-admin prepare /absolute/path/perry-infra-isolation-release
   ```

   It takes all four application release locks plus the infrastructure lock,
   checks the old dependencies and new subnet availability, and retains the full
   service specifications, installation, public release key and original policies
   under root-only `/var/lib/perry-network-isolation`. Existing migration state
   is never overwritten. It installs the reviewed infrastructure broker binary,
   creates `/opt/perry-infra-release/network-maintenance`, creates/validates the
   overlays and adds new interfaces to shared services while retaining the old
   interfaces. Public health checks run after each shared-service update.
   It leaves the old installed snapshot in place.

   The updated broker rejects ordinary infrastructure CI while the marker exists,
   so the old snapshot cannot undo the intermediate state. Its root-only
   `network-finalize` command still requires the reviewed fingerprint and lock.
   The restricted SSH parser and sudo rule cannot invoke that command.
   New runtime settings are in the already-fingerprinted compose/HBA files;
   the immutable HBA config is versioned to `postgres_hba_isolation_v3` and the
   existing installer installs both files. No CI account can refresh them.

2. Migrate **one application**, starting with Fleet, and finish its functional
   checks before moving to the next. Use this sequence: `fleet`, `chinwag`,
   `vita-track`, `im-listening`.

   ```sh
   sudo /absolute/path/perry-network-admin cutover fleet
   ```

   A cutover rejects any service change since the retained inventory, submits one
   version-checked update replacing its old networks and selecting its ingress
   label, waits for the healthy task, then checks public health, direct-IP denials,
   its database/migration privileges and applicable Redis ACLs. Full cross-database
   HBA denials run after installing the final HBA. A failure stops the sequence;
   inspect privately and use rollback below instead of blindly retrying.

   Perform the app's authentication/core-flow checks in the evidence table below.
   Then review the live spec and record its new image-only release policy:

   ```sh
   sudo /usr/local/libexec/portfolio-release record fleet
   ```

   Before recording, the old policy deliberately rejects releases of the changed
   service. Recording remains an administrator action. Substitute the selected
   app for the other three cutovers and policy records. Retain the old policies.

3. After all four apps pass, install the final reviewed infrastructure snapshot:

   ```sh
   sudo sh /absolute/path/checkout/deploy/install.sh /absolute/path/perry-infra-isolation-release /var/lib/perry-network-isolation/release.pub
   ```

   Compute the expected fingerprint from the same reviewed checkout on the trusted
   builder, then pass that exact digest, not a digest blindly read from the VPS:

   ```sh
   go -C deploy/infrastructure run . fingerprint ../..
   sudo /absolute/path/perry-network-admin finalize EXPECTED_64_HEX_FINGERPRINT
   ```

   The installer retains the maintenance marker. Finalization uses the restricted
   snapshot's normal fingerprint/lock checks through the administrator-only mode,
   removes the shared services' old network attachments, installs the new HBA,
   and runs full topology/client/public checks. It retains both old empty overlays
   and the maintenance marker pending the remaining evidence.

4. Check all required functional/monitoring/release/rollback evidence. Re-run:

   ```sh
   sudo /absolute/path/perry-network-admin verify
   sudo /absolute/path/perry-redis-admin verify /absolute/path/checkout
   sudo /absolute/path/perry-metrics-admin verify /absolute/path/checkout
   ```

   Build the updated Redis administrator helper from this checkout first. Its
   verifier selects the one reviewed client overlay actually shared by Redis and
   the API; it no longer assumes `redis_clients`. It creates only a temporary
   verification job using the existing separate probe user. The historical Redis
   migration commands still describe the old cutover and are not this migration.
   The metrics administrator tool's syntax is documented in its runbook; retain
   its root-only evidence logs. Confirm all five external health monitors and
   Netdata Cloud collectors/alerts recover; do not deliberately fault production.

5. Record exact timestamps/results below and in `docs/tasks.md`. Once the owner
   has accepted the results, remove only the maintenance marker as administrator.
   Verify the real infrastructure restricted release path accepts the installed
   fingerprint and rejects an uninstalled one. For each app, verify its protected
   image release using its current digest/revision and its retained rollback image,
   then return to the desired image and recheck health/core flows. Run these via
   the actual protected workflows or supported restricted accounts. Do not
   substitute an unrestricted Docker update for release-broker verification.

6. Retain both old overlay IDs until the owner retires the network rollback
   window. Before deletion, confirm **every** service specification and running
   task has no attachment to `portfolio_network` or `redis_clients`. Inspect both
   networks' container endpoints as well; a remaining task/endpoint is a blocker.
   The full topology verifier rejects any service that still uses either overlay.
   Then remove only those two empty overlays as administrator. Keep the retained
   specifications, images, installation and all data. Removing the old network
   IDs ends automatic specification rollback; the helper refuses rollback if
   any referenced network/config/secret or retained image has been retired.

## Rollback and interruption

Before retiring either old network, use:

```sh
sudo /absolute/path/perry-network-admin rollback
```

This is a full rollback of this migration: it checks all original resource IDs,
service IDs and cached images **before changing anything**, holds every release
lock, restores shared-service paths before the original application specifications,
and restores the original infrastructure installation and four broker policies.
Current PostgreSQL and Redis data stay in their existing volumes, including all
writes since cutover. No credentials or database ownership are changed. Shared
service restarts and app task replacements cause the same short interruptions
as the forward migration. Repeat all public, authentication, core-flow and
monitoring checks, and verify restricted releases against the restored policies.

An interrupted operation retains root-only state and the maintenance marker.
Do not remove the marker to make CI proceed. Use the saved inputs to roll back,
inspect the cause, then retain/rename the completed audit directory before a new
attempt. If `backed-up` is absent, preparation failed before any live mutation;
review the incomplete private directory before moving it aside. Once old network
IDs are deleted, reconstruct and review new memberships/specifications through
administrator recovery; do not bypass the helper's missing-resource refusal.

Before claiming rollback works, perform the full rollback drill while overlays
are retained, repeat the baseline checks, then rerun the staged cutover from a
new audit directory. Keep both audit directories and record both drill timings.
Image rollback drills separately verify the app release brokers; neither drill
restores old database contents.

## Required production evidence

Local preparation on 2026-11-12 passed the isolation Docker fixtures and race
tests, infrastructure broker race tests, Redis helper race tests, all three Go
modules' vet checks, and the existing disposable metrics/Netdata integration
suite. All five stack manifests validated using empty fixture env files; Linux
amd64 builds succeeded. Read-only public checks returned HTTP 200 for all five
production health URLs. These are preparation/baseline results, not cutover or
authenticated production acceptance.

After review on 2026-11-12, readiness retry regression tests passed repeated
transient HTTP/connection failures, request timeouts, cancellation and the shared
deadline. The isolation Docker/race tests, infrastructure and Redis race tests,
and all three modules' vet checks passed again; the Linux amd64 helper rebuilt.

The guided flow subsequently passed race/vet checks and regression tests for
per-app acceptance gates, pause/EOF/resume, failure and rollback handling,
input-bound progress, bundle integrity, policy wire compatibility and held
release locks. Linux isolation and portfolio broker race/vet checks passed with
Go 1.27.3. The real disposable isolation and private-metrics Docker suites passed
again, as did infrastructure and Redis race/vet checks. This tests the prepared
flow and fixtures; no production guided run has taken place.

The prepared package is staged at
`/home/pez/perry-network-isolation-20261112-recovery.GmLgRU` on `pez-vps`, with source,
coordinated application manifests, four Linux binaries and `SHA256SUMS`.
This package adds clean-client recovery, progress reporting and checked adoption
of the paused initial preparation. It supersedes the retained
`/home/pez/perry-network-isolation-20261112-guided.3pV88C`,
`/home/pez/perry-network-isolation-20261112-retry.Tm6cAS` and
`/home/pez/perry-network-isolation-20261112.CDXLIZ` packages.
The target runtime fingerprint is
`b299443ef76b27a11c99964b3aede0322a64e96c5bfb7add8416de2747cf51d5`.
The first attempt updated Traefik at 18:26:57–18:27:34 UTC, PostgreSQL at
18:27:35–18:28:20 and Redis at 18:28:21–18:29:01. Im Listening exited cleanly at
18:29:02 after dependency interruption; its on-failure policy left it at 0/1.
The preparation health gate then failed with HTTP 404. The owner selected stop;
no app cutover was accepted at that point. The replacement Im Listening task
started at 18:45:38 UTC; it was subsequently observed healthy and its public
health URL returned HTTP 200. The checked recovery command completed preparation
and Fleet's ingress cutover; the owner is waiting at `drill/check/fleet`.
The recovery code passed regression/race/vet tests and the disposable Docker
suite, including exact-spec restoration and refusal to adopt later progress.

For this paused attempt, run the following from your workstation after the old
runner has exited:

```sh
ssh -tt -o IPQoS=none -o ServerAliveInterval=15 -o ServerAliveCountMax=3 pez-vps 'sudo -p "VPS sudo password: " /home/pez/perry-network-isolation-20261112-recovery.GmLgRU/perry-network-admin resume-preparation b299443ef76b27a11c99964b3aede0322a64e96c5bfb7add8416de2747cf51d5'
```

This command verifies the original bundle binding and fully applied intermediate
topology before recovering clients and completing preparation. It retains the
old progress JSON, then binds continuation to this reviewed recovery package.
It refuses adoption after any app cutover/acceptance or fingerprint change.
Follow the named prompts; the runner executes the administrator stages above.
Keep this package intact for resume/recovery. For manual recovery, use its
`checkout` directory, `perry-isolation-metrics-admin` and updated
`perry-isolation-redis-admin` binaries.

Every entry is **pending** until actually observed. Health endpoints alone do not
prove authenticated application behavior. Use controlled accounts/test records;
remove disposable state afterward and never record credentials or session data.

Fleet drill verification on 2026-11-12 (completed around 19:11 UTC): the real
production browser showed five moving vehicles, status changes and Live; search
focused V-101, V-103 and V-102 with map popups, and a no-match query displayed
No vehicles found. Reload re-established Live and moving telemetry. A separate
Go HTTP probe used the normal anonymous viewer/session-token flow, consumed
eight snapshots across two signed streams, observed all five vehicles moving
on each stream and 39 road-snapped vehicle observations, closed the first
connection and reconnected with a fresh token. Its first token's replay was
rejected with HTTP 401. Missing/invalid service credentials, missing/invalid
stream tokens and token issuance without a viewer session also returned 401.
The live app's OpenRouteService configuration was present (no values printed),
and its sole service overlay was `fleet_ingress`. Tokens/cookies stayed in
memory; no production data or service configuration was changed by these checks.
This verifies reconnect via reload/fresh connection, without inducing an outage
or claiming a forced-network-failure retry test. Final-pass checks remain pending.

Chinwag drill verification on 2026-11-12: created two explicitly authorized,
reusable controlled accounts (`infra_test_a_20261112` and
`infra_test_b_20261112`). Their credentials are retained only in a mode-0600 file
under the private local configuration directory, outside Git. The HTTP/Socket.IO
probe kept its cookies and tickets in memory; credentials/session values were
not printed or added to Git. Browser signup, logout, rejected
wrong password and correct-password login passed. The actual Socket.IO client
library established two authenticated WebSockets and automatically reconnected
after closing only its own test transport, obtaining a fresh socket ticket.
Missing/invalid ticket handshakes were rejected. A labelled message was sent
through the browser from test A to test B, a controlled reply arrived live, and
both were confirmed in API history and in the browser after full reload.
Unauthenticated profile, messages and ticket-issuance routes returned HTTP 403.
Live membership was exactly `chinwag_ingress` plus `chinwag_database`; all ten
services were 1/1. The accounts and two labelled messages are intentionally
retained for future tests; existing conversations were untouched. Final-pass
checks and the overall task remain pending.

VitaTrack drill verification on 2026-11-12: created the authorized reusable
`InfraTest20261112` account with a synthetic profile, one explicitly labelled
synthetic workout and one labelled synthetic food/nutrition entry. Credentials
are retained outside Git in a private mode-0600 local configuration file;
HTTP probe cookies remained in memory. Browser signup/setup, logout and fresh
password login passed. The Go HTTP probe rejected an incorrect password,
verified the authenticated account identity and ownership of its saved records,
and read both today's and historical activity/nutrition through the normal
frontend GraphQL proxy. The browser showed the workout after reload, including
its exercise, one-minute duration and date. After fresh login and reload, the
food diary showed the synthetic food, 123 calories and matching macro totals,
with 2377 calories remaining against the synthetic 2500-calorie goal. No personal
health records were used. Eight unauthenticated/invalid-cookie activity and
nutrition requests across the API and frontend proxy returned errors with no
protected records; simple GraphQL GETs without preflight headers were rejected
with HTTP 400 CSRF errors on both endpoints. Public health returned HTTP 200,
live app membership was exactly `vita_track_ingress` and `vita_track_database`,
and all ten services were 1/1. The runner reported its transition database,
migration and direct-IP isolation checks passed. The test account and labelled
records are retained for future verification; final-pass checks remain pending.

Im Listening drill verification on 2026-11-12 (completed around 20:00 UTC):
the existing `Deployment Test` browser session remained authenticated after
cutover and full reload. Logout followed by returning Google sign-in using the
already linked application Google account returned to the same existing account
and sign-in settings; no onboarding, linking or permission expansion occurred.
Created the authorized reusable `infra_test_20261112` password account with
credentials retained only outside Git in a mode-0600 private local file.
Browser signup, rejected incorrect password, correct password login and reload
passed. The Go HTTP probe independently verified login/rejection, two Redis
session reads, the Secure/HttpOnly/host-only/Path=/SameSite=Lax cookie attributes,
logout and denial of the old cookie after logout. Anonymous Spotify requests
returned 401; anonymous/invalid-cookie GraphQL writes returned UNAUTHENTICATED,
foreign origins and non-JSON writes returned 403, production introspection was
disabled, and direct Google requests without frontend-proxy authorization
returned 403. A separate disposable Google flow verified start/S256 PKCE,
form-POST receipt handling, cancellation and receipt replay denial, then logged
out its temporary session. Real returning Google sign-in exercised the session
completion Lua path. Authenticated Spotify API search returned five tracks and
the artist genre endpoint returned 200 with an empty genre list. Initial API
and browser search attempts failed/showed unavailable; subsequent identical
`Massive Attack Angel` searches and a `Radiohead Creep` browser search returned
five tracks, and track selection reached the recommendation form. No service
change was needed for recovery; the cause of the temporary search failures was
not established. No post was published, and reload cleared the unpublished
draft (an empty composer was observed). The reusable test account is retained.
Twenty-one invalid Google-start requests from this check's own client address
triggered HTTP 429 with Retry-After 59 and RateLimit headers; after the window,
the same invalid request returned its normal 400 again. This exercised the real
Redis-backed HTTP limiter without repeated provider authorization or mailbox
traffic. Public health returned 200 before and after verification. Live app
membership was exactly `im_listening_ingress`, `im_listening_database` and
`im_listening_redis_clients`; all ten services were 1/1. The runner reported
transition database/migration/Redis and direct-IP denials passed. Final-pass,
final HBA/ACL and full rollback-drill evidence remain pending.

The owner accepted all four drill app prompts and completed `drill/install`.
At `drill/finalize`, shared-service restarts again cleanly stopped Im Listening.
Its recovery submitted zero and one replicas without an orchestrator barrier,
then timed out. Read-only inspection found a requested replica count of one,
zero running tasks and only an explicitly clean, retired shutdown task, with no
replacement task. This is consistent with Swarm coalescing rapid scale updates;
increasing the health timeout does not address the missing task. The updated
Go recovery at that point waited for all old task history and containers to disappear before
restoring the exact original one-replica specification using a fresh service
version. It also recognizes an explicitly clean retired task after an
interrupted scale cycle. Concurrent spec changes still fail closed, and a
scale-down timeout restores the requested count when the exact zero spec is
still present, without accepting the failed step.

The later final-preparation attempt exposed a retained-history case missing from
that first fixture. The current recovery described at the top of this runbook
waits only for the specific completed task plus the absence of live tasks and
containers; it preserves unrelated historical tasks. Its expanded real Swarm
fixture includes retained history and reproduces the case that blocked this
older build.

Regression/race/vet checks passed on macOS and Linux, and a disposable nested
Docker Swarm fixture verified clean completion, observed task removal,
replacement startup and byte-identical final service specification. The local
host's Swarm was not enabled or modified. The recovery build is staged
separately at
`/home/pez/perry-network-isolation-20261112-finalization.Dbc1Xs`;
its Linux amd64 network-admin SHA256 is
`d989a47956cc774467f091cc0dad8f9d097d1ddaf10c3270934ba9073ba57320`.
The original recovery bundle and root-only progress/rollback inputs are intact.

At the failed-step prompt, enter `stop` to release the application locks while
retaining maintenance, then run:

```sh
ssh -tt -o IPQoS=none -o ServerAliveInterval=15 -o ServerAliveCountMax=3 pez-vps \
  'sudo -p "VPS sudo password: " /home/pez/perry-network-isolation-20261112-finalization.Dbc1Xs/perry-network-admin resume-finalization b299443ef76b27a11c99964b3aede0322a64e96c5bfb7add8416de2747cf51d5'
```

The new command can adopt only the pending drill/final finalization at the same
reviewed fingerprint. It verifies both bundle bindings, the installed snapshot,
all four saved cutovers and exact service specifications before archiving the
old progress and updating only the executable binding. It retains the cursor,
accepted checks and original rollback inputs, then retries the failed
finalization. Once adopted, rerun this same command after later pauses. This
recovery was subsequently run by the owner: Im Listening restarted successfully,
and `drill/finalize` and `drill/verify` passed. Computer Use had refused Ghostty
access for safety reasons, and noninteractive SSH sudo required a password; no
production Docker mutation was performed through the unprivileged connection.

Post-finalization checks on 2026-11-12 (around 21:38–21:50 UTC): all ten services
were 1/1. UptimeRobot showed 0 Down, 5 Up and 0 Paused, including Im Listening's
recovery. Netdata Cloud showed `my-vps` Live with fresh PostgreSQL connections,
transactions, held/awaiting locks and deadlock charts; Redis memory, clients,
lookup hit rate and eviction charts; and native Traefik entrypoint plus
Prometheus service request/latency charts. Samples continued through 21:45 UTC.
Observed Redis samples included 1.6329 MiB used, two connected clients, 72.6%
lookup hit rate and zero evictions. Both service request and latency-count label
menus contained all five application services. Cloud showed no raised alerts;
Running contained Clear evaluations for the PostgreSQL connection, acquired-lock
and per-database deadlock rules, plus `perry_redis_memory`,
`perry_redis_evictions` and `perry_traefik_5xx`. Public HTTPS port 443 was reachable,
while all eight reviewed database, private metrics and Netdata ports were
unreachable externally. No alert incident was deliberately induced.

Post-restart application probes re-established two signed Fleet streams with
eight moving-vehicle snapshots and a rejected token replay. Chinwag's actual
Socket.IO client authenticated both controlled accounts, reconnected
automatically, rejected missing/invalid tickets and read the two retained test
messages. VitaTrack authenticated its test account and read the retained
workout/nutrition through both history and today's queries, without writing
records. Im Listening retained the browser's pre-restart Infrastructure Test
session; independent password/session/logout, origin/introspection, Google
start/cancellation/receipt-replay and Spotify track/genre checks passed. One
Spotify request returned 502; a repeat passed, and a diagnostic using the app's
actual Node fetch in its container obtained provider token/search HTTP 200
without printing credentials or tokens. No test post was created.

Fleet produced zero road-snapped observations in the repeated stream check.
A diagnostic using the app's configured credential and actual Node fetch from
inside its container received HTTP 403 from OpenRouteService with a quota error
for a single public London test coordinate. The provider was reachable; road
snapping is degraded while live vehicles/SSE continue. No credential, network
or service configuration was changed. The owner subsequently accepted
`drill/monitoring` and continued the rollback drill with this external quota
issue recorded. The overall task and final-pass verification remain open.

Restored-baseline verification on 2026-11-12, approximately 22:03–22:07 UTC:
the phase runner reached `drill/restored-check` after automatic rollback and
baseline/private data/metrics verification. Read-only inspection confirmed all
ten services 1/1 and the original app memberships (`portfolio_network`, plus
`redis_clients` for Im Listening). All five public health URLs returned 200.
The batched app probes re-established two Fleet streams with eight snapshots
and ten moving-vehicle observations across the two streams; token replay
returned 401. Chinwag authenticated both retained controlled accounts, rejected
missing/invalid socket tickets, automatically reconnected its Socket.IO client
and read the two retained test messages. VitaTrack authenticated its test
account and read its existing workout/nutrition from today's and history
queries without writing records. Im Listening's pre-rollback browser session
still identified Infrastructure Test and showed the existing six-post feed;
an independent controlled password login read the Redis session and those six
posts twice, then logged out its own probe session. No signup, linking, public
post or new application record was created.

UptimeRobot showed 0 Down, 5 Up and 0 Paused. Netdata Cloud's PostgreSQL
connection/lock/deadlock rules and custom Redis memory/eviction and Traefik 5xx
rules were running and Clear. Fresh Cloud samples included PostgreSQL
connections at 22:05:50, Redis connections/clients (two connected) at 22:06:20,
and Traefik service request/duration series at 22:06:50 UTC. The independent
automatic private collector checks had already completed before this manual
checkpoint. Fleet still reported zero snapped observations; the previously
diagnosed provider quota issue remains unresolved, and no additional provider
authorization or quota diagnostic was repeated for this short baseline check.
The restored baseline passed with that existing exception recorded. The owner
accepted checkpoint 2/3, archived the drill and continued the final rollout.

Final combined checkpoint verification on 2026-11-12, 23:28–23:36 UTC:
the recovery runner reached `accept` after its automatic final app cutovers,
snapshot installation, shared-service finalization, complete topology and private
PostgreSQL/Redis/collector checks. Independent read-only service inspection showed
all ten services 1/1 and the exact reviewed app memberships: Fleet ingress only,
Chinwag and VitaTrack their own ingress/database pairs, and Im Listening its own
ingress/database/Redis-client overlays. Each selected Traefik network matched its
app ingress. All five public health URLs returned 200 again at 23:36 UTC. Public
TCP probes could establish the HTTPS baseline but could not connect to ports
5432, 6379, 8080, 8082, 15432, 16379, 18082 or 19999.

Fleet's probe consumed eight moving-vehicle snapshots across two fresh signed
streams, observed five moving vehicles on each, rejected token replay with 401,
and reconnected with a fresh token. Five negative authentication requests
(missing/invalid service credentials, missing/invalid stream tokens and token
issuance without a viewer session) returned 401. The browser reloaded to Live,
showed changing telemetry, found V-103 and focused its vehicle popup. There were
zero snapped observations. A single fixed public-coordinate request through the
actual app's Node fetch/configuration received HTTP 403 with a provider quota
error. Road snapping is explicitly an unresolved functional exception; provider
reachability and working live updates do not count as successful road snapping.

Chinwag authenticated both retained accounts, established two authenticated
WebSockets, rejected missing/invalid tickets and automatically reconnected after
closing the test client's transport. Its retained browser session reloaded and
read the prior conversation. A controlled UI send reached the other test account,
the reply reached the first socket, and both new message IDs persisted in
history. These two labelled smoke-test messages remain in the retained test-only
conversation for future checks. VitaTrack's retained browser session reloaded
and showed the labelled workout and 123-calorie nutrition record. Independent
controlled login, today/history reads and ownership checks passed without new
record writes. Eight anonymous/invalid-session workout/nutrition reads were
denied across API and frontend, and both simple GET CSRF probes were blocked.

Im Listening's pre-final-cutover Infrastructure Test browser session remained
authenticated and showed its existing sign-in settings. Independent password
login/rejection, two Redis session reloads, secure host-cookie attributes,
logout and old-cookie replay rejection passed. Anonymous session/Spotify access,
foreign origins, introspection, direct Google proxy bypass and four anonymous/
invalid-cookie protected mutations were rejected; two non-JSON writes were
blocked. Authenticated Spotify search returned five tracks with HTTP 200 and
the genre lookup returned 200. Google start/S256 PKCE, cancellation, callback
receipt replay rejection and disposable probe-session logout passed. A browser
onboarding flow was cancelled without creating an account; returning Google
sign-in through the already linked identity then reached the existing Deployment
Test account and connected sign-in settings. No new signup, provider linking or
public post was completed. Twenty-one invalid Google-start requests from the
probe's client address returned 429 with Retry-After 58 and RateLimit headers;
after the window the invalid request returned its expected 400 again.

UptimeRobot showed 0 Down, 5 Up, 0 Paused. Netdata Cloud showed fresh Traefik
request/duration series at 23:31–23:32, PostgreSQL connection and lock/deadlock
series at 23:32 (acquired lock utilization 0%), and Redis samples at 23:33–23:34
(two connected clients, approximately 1.64 MiB used and zero evictions). The
Raised view reported no active alerts. Running PostgreSQL connection/lock/
deadlock and other database rules were Clear (six per-database evaluations where
applicable); custom Redis memory/eviction rules were Clear and Traefik 5xx was
Clear for all four monitored entrypoints. Browser screenshots are retained
outside Git in the local `final-acceptance` evidence directory. No alert
configuration or notification settings were changed. The combined checks passed
with the previously accepted Fleet provider-quota exception explicitly retained.
The owner subsequently entered `pass accept` and the runner completed final
verification, recorded the reviewed image-release policies, cleared maintenance
and reported successful completion. Protected CI release/rollback checks are
now in progress; old overlays and both audit sets remain retained. No task
checkbox is closed.

| App | HTTPS, auth and core flow | Database/migrations and isolation | Redis | Monitoring, restricted release and rollback |
| --- | --- | --- | --- | --- |
| Fleet | Final live updates, reconnect, search and protection passed; ORS quota 403 still degrades road snapping (recorded exception) | Final automatic isolation verification passed; independently confirmed ingress-only membership | N/A | Final external Up and Cloud charts/Clear alerts checked; protected release/rollback evidence pending |
| Chinwag | Final retained session, two controlled logins, two-way message delivery/persistence and automatic Socket.IO reconnect passed | Final automatic DB/migration/denial checks passed; independently confirmed ingress/database membership | N/A | Final external Up and Cloud checks passed; protected release/rollback evidence pending |
| VitaTrack | Final retained session, activity/nutrition reads and GraphQL protection passed | Final automatic DB/migration/denial checks passed; independently confirmed ingress/database membership | N/A | Final external Up and Cloud checks passed; protected release/rollback evidence pending |
| Im Listening | Final retained session, password/rejection/logout, returning Google sign-in, Spotify search and protection passed | Final automatic DB/migration/denial checks passed; independently confirmed three-overlay membership | Final private verification, session reloads, logout/replay and HTTP 429/Retry-After/recovery passed | Final external Up and Cloud charts/Clear alerts checked; protected release/rollback evidence pending |

Also record exact overlay membership/encryption/non-attachability, no published
PostgreSQL/Redis ports or host listeners, all public monitor statuses, repeated
numeric Netdata samples and alert recovery, current/rollback image digests and
workflow runs, installed snapshot fingerprint, downtime and rollback-drill
results. Only then close the task checkbox.

Design references: [Docker Swarm networking](https://docs.docker.com/engine/swarm/networking/),
[Traefik Swarm network selection](https://doc.traefik.io/traefik/v3.4/providers/swarm/),
and [PostgreSQL HBA matching](https://www.postgresql.org/docs/17/auth-pg-hba-conf.html).
