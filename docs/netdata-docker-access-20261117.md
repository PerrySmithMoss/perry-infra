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

## Startup verification and recovery

The first administrator attempt at approximately **14:29 UTC** reached the
filtered API from a group-free Netdata process and removed the membership. Its
immediate process-count assertion ran before all monitoring processes had
finished starting. The migration automatically restored the original membership,
configuration and infrastructure fingerprint, restarted monitoring and passed
both private-collector and Docker-collector rollback verification. Evidence is
retained privately at
`/var/backups/perry-maintenance/docker-api.2213389064`.

The verifier now waits for the actual Netdata and three relay service MainPIDs
to finish starting as the Netdata user, while checking all Netdata processes
for both primary and supplementary Docker-group credentials.

## Production result

The corrected revision `13405f8d611e330637bd960adcc903b8aa904bce`
passed the required `scan` from GitHub Actions (app 15368) in
[run 44104783966](https://github.com/PerrySmithMoss/perry-infra/actions/runs/44104783966).
The owner ran the reviewed `drill-and-install` command at approximately
**14:43–14:51 UTC**. Both the complete group-free verification and the controlled
rollback verification passed; the command then repeated the cutover and passed
the final verification. The drill's printed “Verification failed” line is its
deliberate rollback trigger, followed by an explicit controlled-drill pass.

Private evidence and rollback records remain outside the checkout:

- Controlled drill: `/var/backups/perry-maintenance/docker-api.1887411580`.
- Final installation and manual rollback: `/var/backups/perry-maintenance/docker-api.866299439`.

The administrator snapshot changed from
`3f9df3a1754cc9e855595fd1e66ad95e6c50860a5b372c4c21eefab524317bf2`
to `c12e21921fe824da9ba34661f85e9a391c7382b42a7b1928157574c2c46c620d`.
The installed Docker-reader binary SHA-256 is
`aae50e4e7544a367c453ff1c24a1d38ab2546e4d0de38aca0cac8de917ce66f5`.
The existing release broker was replaced atomically with the reviewed binary
that fingerprints the nine new runtime files. Existing runtime files, release
credentials and SSH policy were preserved. The maintenance marker was cleared
after final verification.

Administrator verification established allowed reads, response-field projection,
write/unrelated-read denials and `EACCES` on the raw Docker socket in a fresh
Netdata-user process with no supplementary groups. It checked every current
Netdata process for primary/supplementary Docker credentials and resolved all ten
container names through the actual installed cgroup-name helper. Both passes
verified repeated numeric Docker health/state samples and the four private
PostgreSQL, Redis, Traefik and Prometheus collectors, including PostgreSQL TLS
and the restricted monitoring role.

Independent SSH checks after the final cutover confirmed UID/GID 987 and only
group 987 for Netdata and all three relay MainPIDs. The filtered socket is
root:netdata 0660; the agent has the filtered `DOCKER_HOST` and masks the raw
socket. Ports 15432/16379/18082 listen only on loopback, with no 19999 listener.
All ten Swarm services remained 1/1; all seven containers with probes were
healthy. The running kernel and boot ID remained 56 and
`08f78547-64c2-4c8a-bc3d-7fae8e9e0fc6`. No application deployment or host reboot
was part of the cutover.

The external checker at **14:50:59 UTC** passed all five public checks, including
BulkBets' `A-OK!` response. Ports 80/443 were reachable and
5432/6379/8080/8082/15432/16379/18082/19999 were blocked externally.

Traefik's existing proxy ACL remained byte-identical. Its service specifications
were unchanged before, during and after the migration (SHA-256 of the formatted
Docker `.Spec` output):

- Traefik: `fec16b9d8f78cf1bd90d2685dc6395b92790da85255fe1445571f9c9069c4611`.
- Existing socket proxy: `6e03198d3053fedac37610d25c4c1eb997a924405aaaf79c9137ad682278d31e`.

Cloud showed the running `docker/local` job coming from a user file and targeting
`unix:///run/perry-netdata-docker-api.sock`, with update interval 10 seconds,
detection retry 30 seconds and timeout 2 seconds. Docker state/health samples
were fresh at **14:50:40 UTC**, including ten running containers. PostgreSQL had
one running explicit private job; its redundant failed Docker-discovery job was
gone. PostgreSQL, Redis and service request/latency samples were fresh after the
final restart.

Traefik entrypoint request/latency samples were fresh at **14:54:56 UTC** and
Prometheus service request/latency samples at **14:54:00 UTC**. The cgroup table
retained all ten friendly container names with CPU, memory and disk/network
columns; the ten-instance CPU chart had fresh samples at **14:55:28 UTC**.
At **14:56:01 UTC**, the Cloud Raised tab reported no active alerts and 74
running alert evaluations. No alert definition or threshold was changed.

## Acceptance

- [x] Implement a separate Unix-only, allowlisted and response-filtering broker.
- [x] Test actual unprivileged Netdata Docker health/state collection and cgroup names, plus API/metadata denials, using disposable local containers.
- [x] Pass the protected PR scan and install the reviewed host/snapshot configuration through the administrator path.
- [x] Pass a controlled production monitoring rollback drill and the final group-free cutover.
- [x] Verify actual process groups/raw-socket denial, all collectors and Cloud freshness, public endpoints/private ports and unchanged Traefik/proxy specifications.

Retain the final private rollback directory and staged administrator tools for
recovery. Review later host changes before a manual rollback; do not restore
the first-attempt or drill record over the completed installation.
