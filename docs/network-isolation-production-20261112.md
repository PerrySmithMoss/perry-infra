# Network isolation production evidence

The owner completed the guided cutover and final acceptance on 2026-11-12.
The complete service-specification rollback drill and restored-baseline checks
passed before the final rollout. Recovery inputs remain in the root-only
`/var/lib/perry-network-cutover/rollback-drill` and
`/var/lib/perry-network-isolation` audit sets.

## Protected application release and rollback

On 2026-11-12/13, each default-branch production workflow built and released
an image from its already approved application revision, rolled back to the
exact retained image verified during the cutover, and restored the new image.
All twelve workflow runs passed. Each operation used its existing GitHub
production environment, pinned SSH transport, dedicated release account and
image-only broker. The broker verified the image revision and unchanged service
policy; no unrestricted Docker service update was used for this evidence.

| Application | Release | Rollback | Restore |
| --- | --- | --- | --- |
| fleet | [passed](https://github.com/PerrySmithMoss/logistics-realtime-dashboard/actions/runs/43331456265) | [passed](https://github.com/PerrySmithMoss/logistics-realtime-dashboard/actions/runs/43332242675) | [passed](https://github.com/PerrySmithMoss/logistics-realtime-dashboard/actions/runs/43332961983) |
| chinwag | [passed](https://github.com/PerrySmithMoss/chinwag/actions/runs/43331473344) | [passed](https://github.com/PerrySmithMoss/chinwag/actions/runs/43332512372) | [passed](https://github.com/PerrySmithMoss/chinwag/actions/runs/43332966302) |
| vita-track | [passed](https://github.com/PerrySmithMoss/VitaTrack/actions/runs/43331478282) | [passed](https://github.com/PerrySmithMoss/VitaTrack/actions/runs/43332515982) | [passed](https://github.com/PerrySmithMoss/VitaTrack/actions/runs/43332971249) |
| im-listening | [passed](https://github.com/PerrySmithMoss/im-listening/actions/runs/43331482772) | [passed](https://github.com/PerrySmithMoss/im-listening/actions/runs/43332519671) | [passed](https://github.com/PerrySmithMoss/im-listening/actions/runs/43332975311) |

| Application | Approved revision | Retained image digest | Newly released image digest |
| --- | --- | --- | --- |
| fleet | `a623bb02a24db7859569afc14ef1a80796d30db3` | `sha256:c74c3591c5258fd9e2be61b07876d44a9c57a3d4872078a9ed9d1bb1e87ee394` | `sha256:33e5cc1e36f6d16c0b39f7dfe11e4d77a0af7f1f4a7f60caf51c8d46fa85a72d` |
| chinwag | `24eb8c0edac7383778c53fe0507dc302a0d997fe` | `sha256:b52957efb9123c0c23192cd2b4f267bde3ceb6e3461c61288225a599a10b2377` | `sha256:b94bbe4c9e3aea4025d913ee6f2fbdd39bcf194b1536c3109ab7071dca36c36c` |
| vita-track | `c8c3ce4b479e362fe1f1c8d3d2d7ca2ed341e4f1` | `sha256:f1d197d188b51f5c4b040c668181431b0329eef762dcbfc3cea325e98fee9821` | `sha256:a0691138d489097a6fd84f4bad91a448f7cb08354eab8ff6946d3470eae42c81` |
| im-listening | `dae95968390d123be950c6f1555dd72b263ea8c3` | `sha256:e38a00282e0796aa090a26249a2a3da550325f5de5bbafaeadd84c6274c6d082` | `sha256:1d487e4b6bada27d95c8ce7faecf3471a3d342c23fad6ea70fc12ddc71d0fd84` |

Core-flow probes passed on both the new images and retained rollback images:
Fleet established two signed streams, consumed eight snapshots with all five
vehicles moving on each, rejected token replay with 401, and reconnected with a
fresh token. Chinwag authenticated both retained test accounts, opened two
WebSockets, automatically reconnected after closing only its test transport,
rejected missing/invalid tickets and read the four retained labelled test
messages. VitaTrack authenticated and read its retained synthetic workout and
123-calorie nutrition record, including today/history queries. Im Listening
authenticated, read its Redis session and six existing public posts twice, then
logged out the probe session. Credentials and session values remained outside Git.

Im Listening's existing `API_ROLLBACK_ALLOWLIST` entries were preserved and
the two above verified image/revision pairs were added, allowing its rollback
and restore to use the protected workflow's existing approval mechanism.
The independently verified post-source-sync image/revision pair below was also
recorded for future protected rollback, preserving all prior entries.

## Protected infrastructure deployment

[Infrastructure PR #23](https://github.com/PerrySmithMoss/perry-infra/pull/23)
synchronized the already installed snapshot with the protected main branch.
Its required [scan](https://github.com/PerrySmithMoss/perry-infra/actions/runs/43332354684)
passed, including the real disposable PostgreSQL/Redis isolation fixture,
release-policy tests and runtime image scans. The reviewed checkout and merged
source tree have the installed runtime fingerprint:

`b299443ef76b27a11c99964b3aede0322a64e96c5bfb7add8416de2747cf51d5`

The protected [matching deployment](https://github.com/PerrySmithMoss/perry-infra/actions/runs/43333175578)
passed from commit `8e1d40c136aadcd615c3b0623b7949eb8b3b58e5`.
It used the normal required production-environment approval and restricted
`infra-release` SSH account. The application services and data remained intact.

The negative check reran the prior deployment job in
[run 43269234980, attempt 3](https://github.com/PerrySmithMoss/perry-infra/actions/runs/43269234980).
It requested the old source fingerprint
`438cdfb41f25a6e3c332153bf479d4c59b88ae34dcf9222960cfaec3f6abb5ae`.
The installed broker rejected it with
`an administrator must install this exact infrastructure snapshot first`.
The expected failed run is evidence of the guard; it is not a deployment failure
of the accepted snapshot.

## Coordinated source manifests

Each application manifest now records its live per-app ingress and applicable
database/Redis networks, including explicit Traefik network selection.
The changes preserve the current default branch's unrelated service settings.
All four source-sync PRs passed their checks and merged:

- [logistics-realtime-dashboard #8](https://github.com/PerrySmithMoss/logistics-realtime-dashboard/pull/8), merge `7e7ed88ce0420db5f7f50c9a12f4db5ba152903d`.
- [chinwag #8](https://github.com/PerrySmithMoss/chinwag/pull/8), merge `93679aaaa00fdf5364b4b0606649421a6db1fc6b`.
- [VitaTrack #10](https://github.com/PerrySmithMoss/VitaTrack/pull/10), merge `a87a2dcc976f4ff0199bcd7706e67f4e11556874`.
- [im-listening #18](https://github.com/PerrySmithMoss/im-listening/pull/18), merge `2c92ebfe1b0cb494b60b60b72247a8410767ff26`.

Their automatic post-merge production deployments also passed:

| Application | Post-merge production workflow |
| --- | --- |
| fleet | [deployment](https://github.com/PerrySmithMoss/logistics-realtime-dashboard/actions/runs/43333864829) |
| chinwag | [deployment](https://github.com/PerrySmithMoss/chinwag/actions/runs/43333873745) |
| vita-track | [deployment](https://github.com/PerrySmithMoss/VitaTrack/actions/runs/43334149449) |
| im-listening | [deployment](https://github.com/PerrySmithMoss/im-listening/actions/runs/43334156314) |

## Verified promoted images and final reads

The post-merge image digests and OCI revisions were independently checked again
on 2026-11-13 after the interruption. All ten services were 1/1 and all five
public HTTPS health URLs returned 200.

| Application | Promoted image digest | OCI revision |
| --- | --- | --- |
| Fleet | `sha256:efa4d765f5b2d3d50ed48bb06b9740a96992a015f8ea5da1e22df0c683f970b4` | `7e7ed88ce0420db5f7f50c9a12f4db5ba152903d` |
| Chinwag | `sha256:c942fffe8c752127f388ff1a0410702798fa432fef406ef7bcf75d7938c21db5` | `93679aaaa00fdf5364b4b0606649421a6db1fc6b` |
| VitaTrack | `sha256:c80e220848d7fe147cdd7d925b3265a3c5fb1b62520db44dd07565fea00116c2` | `a87a2dcc976f4ff0199bcd7706e67f4e11556874` |
| Im Listening | `sha256:643605cea145bb1b1e611c0382aabe2ceea02bc755d548ba84ca36814f7afb5d` | `2c92ebfe1b0cb494b60b60b72247a8410767ff26` |

At 00:12 UTC the final API probes passed Fleet's two streams/eight snapshots/
token-replay protection, Chinwag's controlled logins/history/automatic reconnect,
and Im Listening's login/two session and feed reads/logout. The retained Fleet
browser was still Live across the releases; the other retained browser sessions
also reloaded successfully. Public TCP probes could establish HTTPS but could
not connect to ports 5432, 6379, 8080, 8082, 15432, 16379, 18082 or 19999.

The VitaTrack probe initially assumed its synthetic records were dated today;
crossing midnight invalidated that assumption. An explicit read for 2026-11-12
subsequently verified the retained owned workout and 123-calorie nutrition
record, history queries, and the correct empty response for the new day.
No records were added or edited to satisfy the check. At 09:34 UTC the existing
Deployment Test Google-linked Im Listening session was still authenticated,
and UptimeRobot showed 0 Down, 5 Up and 0 Paused.

The final Cloud check showed fresh PostgreSQL connection series at 09:39 UTC,
Redis samples at 09:41 (two connected clients), and Traefik request/duration
series at 09:42. The Raised view had no active alerts and 70 rules were running.
All six PostgreSQL rule groups were Clear, including connection/lock utilization
and per-database deadlocks; custom Redis memory/eviction and Traefik 5xx rules
were also Clear. Screenshots are retained outside Git in the local
`ci-release-rollback` evidence directory. Monitoring settings were unchanged.

## Legacy overlay retirement and recorded exception

The old overlays `portfolio_network` (`7cemzepjcvuy1kwks3s8pqcbo`) and
`redis_clients` (`kj9so10l4h424b5e6z5qmc53c`) had zero endpoints and no
service or running-task attachments at 10:07 UTC on 2026-11-13. The single
Swarm node was Ready/Active and all ten services were 1/1. The owner authorized
completing cleanup on 2026-11-13, ending the network rollback window.

At 10:28 UTC, a fresh network inventory confirmed that both old names and IDs
were absent. Separate inspections of each exact ID returned "network not found";
the active `im_listening_redis_clients` overlay remained present. All service
specifications and running containers still had their accepted memberships,
all ten services were 1/1, and all five public HTTPS health URLs returned 200.
The owner's repeated removal command also reported both IDs absent and exited
before running the verifier because it used `set -e`.

The refreshed UptimeRobot dashboard showed 0 Down, 5 Up and 0 Paused. Netdata's
Raised view reported no active alerts at 10:30:05 UTC, 70 rules were running,
and all six PostgreSQL rule groups were Clear. Screenshots remain outside Git
in the local `retirement` evidence directory. No application service restart or
monitoring setting change was needed for retirement.

The normal protected [post-retirement infrastructure deployment](https://github.com/PerrySmithMoss/perry-infra/actions/runs/43410724585)
passed from commit `792cc6fbb7a27d68f17b1ac39901073104ac7446`, using the unchanged
installed fingerprint and normal production-environment approval. Subsequent
inspection still showed all ten services 1/1, all five health URLs returning
200, unchanged running tasks and neither retired overlay recreated.

After that deployment finished, the owner ran the standalone administrator
verifier from the retained `perry-network-isolation-20261112-task-recovery.xmYblp`
bundle. Its complete post-retirement verification passed on 2026-11-13:
encrypted overlays, exact service memberships, explicit Traefik network selection,
unpublished data ports, and final direct-IP denials and applicable
database/migration/Redis checks for Fleet, Chinwag, VitaTrack and Im Listening. It ended
with `Network administrator operation passed`. A separate inspection at
10:50 UTC again confirmed both old overlays absent and all ten services 1/1.
This completes the isolation task's production verification and closure.

Removing the IDs ends automatic full specification rollback. Preserve the
audit archives, retained images, configs, secrets, volumes and all application
data. Protected image rollback on the accepted topology remains available.

Fleet's OpenRouteService quota response (HTTP 403) continues to degrade road
snapping. Live updates, reconnect and vehicle search passed. The owner accepted
the network cutover with this exception explicitly recorded; it is not claimed
as a successful road-snapping check. The app-configured provider probe repeated
on 2026-11-13 also returned 403 with a quota error for one fixed public London
coordinate.

A separate audit finding is that Im Listening's production environment restricts
deployments to `master` and its SSH broker is image-only, but `master` has no
branch protection or repository ruleset. On 2026-11-13 the owner chose to record
this as a follow-up and then expanded it to consistent branch protection across
all six VPS repositories: perry-infra, BulkBets, Chinwag, Fleet, Im Listening
and VitaTrack. The separate task requires an effective-policy audit, required
PRs/checks, administrator enforcement, blocked force-pushes/deletion, and
per-repository enforcement and protected-release evidence. Branch protection
remains unchanged. Fleet's quota exception is also tracked as a separate task.
