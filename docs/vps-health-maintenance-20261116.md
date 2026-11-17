# PostgreSQL healthcheck, Chinwag alert and kernel maintenance

Prepared on 16 November 2026. Reboot into 6.1.0-55 caused a filesystem stall.
Recovery to the retained 6.1.0-54 kernel and application health checks passed.
The PostgreSQL probe fix and Chinwag alert correction are installed and verified.
Privileged service/collector checks passed, and the agent recorded a Clear
evaluation of Chinwag's new rule after its restart. Cloud's rule-match display
disagrees with that agent-side evidence, as detailed below. On 17 November,
the fixed 6.1.0-56 kernel passed a one-time boot and production verification;
the administrator then restored the normal boot default and retained 54 as a
manually installed fallback. The final acceptance evidence is recorded below.

**Do not boot 6.1.0-55 again.** Debian reports a critical ext4 regression in
6.1.192-1: [bug 1154119](https://bugs.debian.org/cgi-bin/bugreport.cgi?bug=1154119)
and [bug 1154140](https://bugs.debian.org/cgi-bin/bugreport.cgi?bug=1154140).
Recovery must use the retained prior kernel through the administrator/provider
console path. Do not attempt a package upgrade or filesystem repair on the
mounted, stalled production filesystem.

The PostgreSQL probe now reads its username from the existing mounted secret and
explicitly selects `postgres`. Compose escapes the runtime command substitution
with `$$`; no credential value enters the manifest or logs. This stops the
ten-second `role "postgres" does not exist` log noise. `pg_isready` remains a
server-readiness probe, rather than an authenticated application-query test.

The same-name Netdata template on `postgres.db_transactions_ratio`, scoped with
`chart labels: database=chinwag`, overrides only Chinwag's stock rollback rule.
It warns above a five-minute average of 10% with a five-minute
notification delay, clears at 5% or below with a five-minute clear delay, and
retains the DBA notification destination. Brief 2--2.5% duplicate-signup bursts
no longer warn. Sustained rollback percentages between 2% and 10% will also no
longer warn for Chinwag; inspect trends and revisit this threshold if workload
changes. Other databases and PostgreSQL alerts retain their stock definitions.
The existing fingerprint and first-install managed-file map already cover
`health.conf`; updates must install it on the host as well as in the snapshot.

## Apply through the administrator path

Do not rerun the first-install metrics command. It intentionally refuses an
existing installation. Build the existing verifier outside the checkout:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go -C monitoring/collectors build -trimpath -o /tmp/perry-metrics-admin .
```

Pause infrastructure releases and other administrator changes during maintenance.
Keep the provider's recovery console available and a separate SSH session open.
Compare the reviewed checkout's old fingerprint with the installed snapshot,
then run the following on the VPS using absolute staged paths:

```sh
sudo sh /absolute/path/reviewed-checkout/monitoring/maintenance.sh \
  862786146e57f5c336fef30c05f1af275b8f4de3808f562d4561db4ab04ec29a \
  /absolute/path/perry-metrics-admin
```

The wrapper refuses snapshot or host-alert rule drift, saves a root-only rollback
archive, reuses the existing broker/key without widening privileges, installs
the reviewed snapshot and host alert, restarts only Netdata and deploys through the
restricted broker. PostgreSQL's stop-first replacement briefly interrupts all
database consumers. Allow a few minutes for deployment and private collector
verification. No application data or credentials are changed. If anything fails,
stop and diagnose before rebooting. Im Listening previously required recovery
after a database restart because its on-failure policy does not restart clean exits.

Preflight on 16 November found that the live alert file's first comment still
mentioned Redis AOF rules, while the snapshot comment correctly describes the
rejected-connection rules. Every alert directive was identical. The comparison
ignores comment-only differences and continues to reject changed alert directives;
the original live file is still included verbatim in the rollback archive.

Verify all ten services have their desired replica and five HTTPS health endpoints
pass. Check PostgreSQL logs since replacement for absence of role errors. In
Cloud, confirm Chinwag's effective rule has 10%/5% thresholds and five-minute
delays, other databases retain stock rules, and metrics update on the same node.
Do not rely on the Cloud editor's match count alone: the discrepancy below was
resolved with the agent's post-restart chart/configuration association and Clear
transition, rather than configuration history or an old Cloud event.

## Reboot and acceptance

The attempted update to `6.1.0-55-cloud-amd64` failed as recorded below. The VPS
has recovered on `6.1.0-54-cloud-amd64`, which is explicitly selected in GRUB.
Do not repeat the reboot into 6.1.0-55. For a future maintenance window, first
verify that Debian has fixed the cited ext4 regression in the candidate kernel,
retain the working kernel and have the recovery console ready. Confirm Docker,
SSH, Netdata and all three metrics sockets are enabled, no package operation is
in progress, no systemd units have failed, and the fixed kernel and initramfs are
installed in `/boot`. An administrator must select the reviewed fixed kernel;
an ordinary reboot currently returns to the retained 6.1.0-54.

After those fixed-kernel preflight, maintenance and Cloud checks pass, schedule a
reboot with a one-minute window to cancel it:

```sh
sudo shutdown -r +1 'Kernel update maintenance'
# To cancel before shutdown starts: sudo shutdown -c
```

All applications share this host and will be unavailable during reboot, normally
for a few minutes. After SSH returns, verify a changed boot ID, the expected
kernel, no reboot-required marker, healthy Swarm tasks, all public health
endpoints, application database connections, private metrics/Cloud reconnection,
and public port isolation. Rerun the staged metrics verifier after reboot.
Do not declare completion from SSH connectivity alone. If boot fails, use the
IONOS recovery console and boot the retained previous kernel; investigate before
changing bootloader defaults or removing kernels.

## Configuration rollback

Use the exact archive path printed by maintenance, with releases still paused.
In an administrator root shell, restore under the release lock:

```sh
backup=/var/backups/perry-maintenance/health.PRINTED_SUFFIX
flock /run/lock/perry-infra-release.lock tar -xzpf "$backup/before.tar.gz" -C /
netdatacli reload-health
/usr/local/libexec/perry-infra-release "$(cat "$backup/fingerprint")"
```

This restores the prior snapshot, broker, release-account files and host alerts;
it does not restore database contents. Deployment can briefly interrupt database
consumers again. Repeat application and monitoring checks. Keep the archive
private and do not restore it over later unrelated infrastructure changes.

## Production acceptance

- [x] Install reviewed snapshot and host alert; verify effective agent rule and record the Cloud display discrepancy.
- [x] Confirm new PostgreSQL task is healthy with no probe role errors.
- [x] Recover the retained kernel and restore all services and public health endpoints.
- [x] Reboot into the updated kernel and verify all applications and collectors.

The administrator installed snapshot
`d3f92b6ee086a977f7fa076119e1eb219fdc9b311433e85e575007d339c2f77e`
on 16 November. The private rollback archive is
`/var/backups/perry-maintenance/health.NQu5Yh4W/before.tar.gz`.
PostgreSQL's new container started at 18:21:46 UTC and reported readiness at
18:21:47 UTC; these timestamps do not establish the full connection interruption.
The new task was healthy with the mounted-username probe and zero logged
ERROR/FATAL/PANIC or missing-role errors at the subsequent check. All ten services
were 1/1, all five public health URLs returned 200, and direct queries through
Chinwag and VitaTrack's deployed database drivers passed. The administrator's
verifier passed Cloud connectivity, TLS, Redis ACL denials, private boundaries,
all required collectors and request/latency metric coverage.

The verifier's initial DIAGNOSIS message came from an older entry in its
append-only operations log; its current Cloud check returned online. Cloud's
Running alerts list showed the rollback alerts Clear, but the selected Chinwag
detail still displayed the old configuration associated with its 16:54:48 event.
That view does not verify the newly installed thresholds. Confirm the effective
configuration after the reboot before closing alert acceptance. The pre-reboot
kernel remained `6.1.0-54-cloud-amd64`, boot ID
`fd8a7b78-510f-402b-af87-c759df5ec76a`; the updated kernel/initramfs were present,
required boot services/sockets enabled, no failed systemd units, and both apt
maintenance services inactive.

The scheduled reboot began at 18:30:24 UTC. SSH was unreachable at 18:30:28 and
returned at 18:30:49 with boot ID `19436110-3698-4726-aafd-d43a947d60fe`, running
`6.1.0-55-cloud-amd64` (6.1.192-1). Applications did not recover: Docker healthcheck
execution stalled, service/API commands hung, and `/proc` showed the ext4 journal
thread (`jbd2/vda1-8`), journald and PostgreSQL in uninterruptible sleep. Load
rose above 48 despite about 870 MiB available RAM, no memory pressure and zero
disk requests in flight. These symptoms strongly match the newly reported
kernel regression, but the restricted SSH account could not read the kernel
crash log. At that stage, recovery and the state of application data were not
established. The previous kernel, configuration archive and production volumes
were preserved for the recovery described below.

## Administrator recovery through IONOS

The owner explicitly authorized use of the signed-in IONOS dashboard and remote
console. Several provider restarts returned to the faulty default kernel because
the console reconnected after the normal GRUB selection window. The console
subsequently displayed kernel oops/register output and processes exiting with
interrupts disabled. The original password retained in IONOS did not authenticate;
no password was changed and no new access credential was installed.

IONOS's `Grml latest_iso` rescue DVD was attached and booted into its existing
administrator shell (Grml 2026.04, kernel 6.19.13). The installed `/dev/vda1`
partition was first mounted `ro,noload` to inspect its boot configuration. Both
6.1.0-54 and 6.1.0-55 kernel/initramfs files and normal GRUB entries were present.
It was then unmounted and mounted normally under the rescue kernel. No fsck,
formatting, database reinitialization, volume deletion or package removal was run.

The original configuration was retained, without overwriting existing backups:

- `/root/grub.before-kernel-rollback-20261116`
- `/root/grub.cfg.before-kernel-rollback-20261116`

Only `GRUB_DEFAULT` in `/etc/default/grub` changed, from `0` to
`'Advanced options for Debian GNU/Linux>Debian GNU/Linux, with Linux 6.1.0-54-cloud-amd64'`.
The diff was inspected before regeneration. `/dev`, `/proc` and `/sys` were
temporarily bound into the mounted installation; `chroot /mnt update-grub`
found both kernels and completed successfully. `grub-script-check` passed and
the generated default named 6.1.0-54. The GRUB environment contained no pending
one-time entry. The disk was synced, all bind mounts and the root partition
cleanly unmounted, and `findmnt /mnt` confirmed it was no longer mounted before
requesting DVD ejection/restart. Normal boot and application recovery verification
remain pending at this point in the record.

This is a temporary administrator boot recovery setting, outside the restricted
application release broker. Keep the known-working kernel selected until a fixed
kernel is verified. Reverting `GRUB_DEFAULT` to `0` now would select the faulty
6.1.0-55 again. After a fixed kernel is available, review its Debian regression
status, schedule a maintenance window with the recovery console ready, restore
the normal default and regenerate GRUB through the administrator path. Retain
6.1.0-54 until the fixed kernel and all applications have passed acceptance.

## Recovery verification

The installed system returned on 6.1.0-54, boot ID
`5512eb02-3684-4005-9491-9c0c6e7e7ae1`; SSH was observed at 19:01:53 UTC.
The rescue DVD was ejected. All ten services recovered, but the published HTTP
ports were absent despite a running Traefik task. A retained failed task reported
`failed to find a load balancer IP` for a network. Recreating only Traefik's task
with `docker service update --force` restored ports 80/443 and HTTPS by 19:05:52.
The before/after service specifications, retained privately under
`/var/tmp/perry-vps-maintenance-20261116`, compared identical after excluding only
`TaskTemplate.ForceUpdate`; no image, networking or other configuration changed.

All five external HTTPS health checks returned 200, all ten services were 1/1,
and current containers had no OOM kills or automatic restarts. PostgreSQL replayed
its WAL and reached readiness at 19:02:54 UTC; the current task logged no
ERROR/FATAL/PANIC or probe role errors. Chinwag and VitaTrack's deployed drivers
queried their respective databases successfully. PostgreSQL SSL was enabled and
the live Netdata session used TLS. Netdata and all three metrics sockets were
active; Cloud reconnected, with only its expected recent-reboot warning raised.
External checks confirmed 443 reachable and all eight private service/monitoring
ports closed or filtered. This is operational recovery evidence, not a complete
audit of application data following the kernel crashes.

Cloud did not show the original instance-specific Chinwag alarm as evaluated.
The final correction uses a database-label-scoped template instead. The new
disposable integration test evaluates the actual rule with a shortened lookup
interval, checking low-traffic suppression, warning entry, hysteresis, recovery,
notification delays and the other database's stock rule source. The actual
Netdata 2.12.3 test passed all four evaluations: 2.22% Clear, 15% Warning, 7%
Warning and 5% Clear; up/down delays remained 300 seconds and VitaTrack retained
the stock source. Go race tests/vet, ShellCheck and snapshot checks also passed.
The final reviewed snapshot fingerprint is
`3f9df3a1754cc9e855595fd1e66ad95e6c50860a5b372c4c21eefab524317bf2`.
The administrator installed this final snapshot, with configuration rollback
archive `/var/backups/perry-maintenance/health.dfZAhUKa/before.tar.gz`.
The complete privileged verifier passed again: all four collectors produced
repeated numeric samples, PostgreSQL TLS/role and Redis ACL checks passed, and
public application and private-network checks passed. At 19:27 UTC all ten
services were 1/1, all five public health URLs returned 200, and the running
PostgreSQL task remained the one started at 19:02:51, with zero logged errors or
probe-role errors. Use `/var/tmp` for the
staged verifier/checkout: the earlier `/tmp` staging directory did not survive
the recovery boots. No further reboot is needed for this monitoring correction.

The initial IONOS credential was not accepted by the installed system, including
a retry using explicit modifier keys to account for remote-console keyboard
mapping. No credential was changed or retained in the repository. Automated
access to the owner's Ghostty app was blocked by the computer-use tool for
safety reasons, so the owner used their own terminal for the final sudo command.
The final installation was performed with:

```sh
ssh -t pez-vps 'sudo sh /var/tmp/perry-vps-maintenance-20261116/apply-monitoring.sh'
```

An explicit Netdata-only restart subsequently succeeded at 19:34:55 UTC
(`ExecMainStatus=0`, active/running). Cloud reconnected with no raised alerts.
The maintenance wrapper now restarts monitoring when installing changed rules.
The rule was also supplied with 45 cheap, read-only Chinwag readiness queries;
no application records were written to generate transaction samples.

At 19:43 UTC, Cloud loaded the intended 10%/5% thresholds and five-minute delays,
but its editor reported that Rule 1 matched no chart and the stock Rule 2 matched
five. The transaction context listed six databases, including Chinwag. Reloading
Cloud did not resolve the discrepancy. The historical Chinwag Clear event was
still timestamped 19:03:40; it cannot prove that the new rule is running. No
configuration was saved through Cloud.

One controlled diagnostic transaction used Chinwag's deployed `pg` library:
the session set `default_transaction_read_only=on`, issued 60 `SELECT 1` readiness
queries, then `BEGIN READ ONLY`, verified `transaction_read_only=on`, performed
`SELECT 1` and explicitly `ROLLBACK`ed. This changed only transaction statistics,
not application records. The previously hidden zero rollback series became
visible in Cloud, but the editor still reported no Chinwag match. The chart's
nonzero display filter means the earlier hidden series alone does not establish
that the agent lacked that dimension.

A temporary Go diagnostic was staged outside the checkout at
`/var/tmp/perry-vps-maintenance-20261116/inspect-alert` (source alongside as
`inspect-alert.go`). It opens Netdata's metadata SQLite database with
`SQLITE_OPEN_READONLY`, rejects non-read-only statements, and prints only this
alert's saved chart/configuration associations and recent configuration history.
It uses the host's existing SQLite library, makes no agent/configuration changes,
and was tested against a disposable fixture whose SHA-256 remained unchanged.
The ordinary SSH account was correctly denied access to the real database, and
the owner ran the diagnostic with sudo. Saved configuration history alone does
not prove evaluation; the chart-linked post-restart transition below does.

The latest saved registration for
`postgres_private.db_chinwag_transactions_ratio` was Clear at
**19:35:12 UTC**, after the 19:34:55 Netdata restart. Its source was
`line=6,file=/etc/netdata/health.d/perry-private-metrics.conf`, chart-label selector
`database=chinwag`, configuration hash `C78866D0669D413B982C1468435C1B40`, warning
expression `$this > (($status >= $WARNING) ? (5) : (10))`, and notification delay
`up 300s down 300s multiplier 1.0 max 300s`. The saved evaluated value was 0%.
All five other databases' latest Clear transitions at the same timestamp used
the stock `/usr/lib/netdata/conf.d/health.d/postgres.conf` source and original
2%/0% thresholds. This establishes that the agent selected and evaluated the
custom rule only for Chinwag after the fresh start; it is not merely a stored
unused definition. The separate Cloud match-count discrepancy remains a display
or synchronization issue; its precise cause has not been diagnosed. No further
host configuration change or restart is needed for alert acceptance.

At **23:48 UTC**, the VPS still ran `6.1.0-54-cloud-amd64`, Netdata was
active/running with exit status zero and the same 19:34:55 start, all ten services
were 1/1, no systemd units had failed, and PostgreSQL was healthy with zero
automatic restarts and no OOM kill. All five external HTTPS health endpoints
again returned 200. The PostgreSQL/alert maintenance is verified; acceptance of
a future fixed kernel intentionally remains open.

The final current-container PostgreSQL log check found zero missing-role probe
errors, zero FATAL/PANIC entries and zero statement timeouts. Five ERROR entries
were all duplicate-key uniqueness violations, consistent with the original
rollback-noise category; no other ERROR category appeared. These are distinct
from the fixed healthcheck's missing-role errors.

Separately, inspection found that the installed `netdata` account already has
supplementary membership in the `docker` group. This maintenance did not add or
use that membership to bypass host privileges. Review it against the intended
monitoring access boundary separately, accounting for the existing Docker health
collector before changing memberships. This is not evidence of new access granted
by this change.

## Fixed-kernel test boot on 17 November

The owner authorized the fixed-kernel maintenance and its planned application
outage. The 17 November advisory check found Debian's merged ext4 regression
reports, including [1154119](https://bugs.debian.org/cgi-bin/bugreport.cgi?bug=1154119)
and [1154140](https://bugs.debian.org/cgi-bin/bugreport.cgi?bug=1154140), marked
fixed in `linux/6.1.192-2`. Debian's
[security-package changelog](https://tracker.debian.org/news/1824723/accepted-linux-61192-2-source-into-oldstable-security/)
identifies the NULL-folio fix. The latest follow-up on
[1154101](https://bugs.debian.org/cgi-bin/bugreport.cgi?bug=1154101) reports that
the final 6.1.192-2 package passed its reproducer. A focused search found no
reported follow-up regression in that version as of this check; that is bounded
advisory evidence, not a guarantee against other kernel defects.

Preflight confirmed the installed `linux-image-6.1.0-56-cloud-amd64` package was
`6.1.192-2`, the faulty 55 package was removed, and both 54 and 56 retained their
kernel and initramfs files. The running 54 boot ID was
`8abd373c-2531-4c82-83f4-1d3871a3676d`. The generated GRUB configuration explicitly
selected 54, supported `next_entry`, and initially had an empty environment.
The provider's `/etc/default/grub.d/15_timeout.cfg` overrides the main file's
five-second setting with an effective zero-second menu timeout. Recovery planning
therefore did not rely on catching the GRUB menu: the one-time selection clears
before loading 56, leaving 54 as the next ordinary boot until promotion.

A fresh signed-in IONOS session identified My VPS at `194.164.126.180`, with no
assigned IPv6 address. Its rescue DVD was ejected, the official Grml image
remained available, and a newly opened remote console connected and responded
at `my-vps login:`. No old console URL/token or initial password was reused.
The console remained available throughout the test boot. All administrator
commands ran in the owner's terminal after sudo authentication.

The installed runtime fingerprint and the retained staged checkout both matched
`3f9df3a1754cc9e855595fd1e66ad95e6c50860a5b372c4c21eefab524317bf2`.
The staged collector Go sources, monitoring verifier/configuration checker and
endpoint inventory matched the reviewed checkout. A fresh static Linux verifier
was built from the merged source using Go 1.27.3, with SHA-256
`6d079a7278cd38fcabf37317528a55f9f2573fef29313a7319faca361d1c70d3`.
Temporary reviewed commands and diagnostics are outside the checkout at
`/var/tmp/perry-kernel-maintenance-20261117`. The prior monitoring snapshot and
Chinwag alert were not reinstalled or retuned.

The administrator preflight held the infrastructure release lock and passed the
complete private-metrics verifier: PostgreSQL TLS and role boundaries, Redis ACL
denials, private overlays/listeners, all public checks, request/latency coverage,
and repeated numeric samples from the postgres, redis, traefik and prometheus
collectors. Docker, SSH, Netdata and sockets 15432/16379/18082 were enabled and
active. All ten services were 1/1, no systemd units had failed, no tasks were in
uninterruptible sleep, the kernel was untainted, dpkg audit was empty, package
locks were unused and both apt maintenance services were inactive. The existing
`unattended-upgrade-shutdown --wait-for-signal` process was an idle shutdown
helper, not a package operation.

At **12:28:01 UTC**, the owner-run command backed up boot configuration privately
under `/var/backups/perry-maintenance/kernel-20261117.Xf5Bl1kt`, set only GRUB's
one-time entry to 56 and scheduled shutdown for **12:29:01 UTC**. The persistent
default remained 54. The shutdown broadcast and SSH disconnect occurred at the
scheduled time; the console showed the new boot progressing. SSH returned by
**12:29:42 UTC** with kernel **6.1.0-56-cloud-amd64** and new boot ID
**`08f78547-64c2-4c8a-bc3d-7fae8e9e0fc6`**. GRUB's `next_entry` was then empty
(`next_entry=`), confirming consumption of the one-time selection.

Public BulkBets probes failed from the first observed check at 12:29:09 through
12:29:55, then returned 200 at **12:30:03 UTC** and on subsequent probes.
These samples establish roughly one minute of planned recovery interruption,
not its exact onset. At **12:30:17 UTC**, all five inventory checks passed:
BulkBets GET returned 200 with `A-OK!`, and the other four HEAD checks returned
200. External TCP checks found 80/443 reachable and all eight private ports
5432, 6379, 8080, 8082, 15432, 16379, 18082 and 19999 unreachable. At 12:30:30 all
ten services were 1/1; all containers with health probes were healthy, with zero
automatic restarts and no OOM kills. Traefik restored its published ports
automatically; no service force-update or specification change was required.

PostgreSQL reached readiness at **12:29:40.236921757 UTC**. At 12:31:51 its current
container had zero missing-role errors, FATAL/PANIC entries, ERROR entries or
statement timeouts. Chinwag, VitaTrack and Im Listening connected through their
deployed `pg` drivers, set default transactions read-only, and successfully read
application migration metadata. The root filesystem was ext4 mounted read/write,
with 57 GiB available. At **12:32:56 UTC**, a temporary 4,400,000-byte file on
`/var/tmp` passed write, fsync, rename, SHA-256 readback and unlink checks. No
existing data was modified by these probes.

Netdata restarted at **12:29:17 UTC**, active/running with exit status zero.
Both reboot-required files disappeared normally. Cloud reconnected and its
pending-package-reboot warning cleared without acknowledgement or silencing;
at 12:32 its only raised warning was the expected recent-reboot alert.

The owner ran the protected post-boot verifier starting at **12:31:37 UTC**.
Its current-boot kernel log contained no BUG/Oops/panic, ext4 error/warning,
aborted journal, read-only remount, blocked-task, I/O-error or watchdog-lockup
patterns. The private full log is
`/var/backups/perry-maintenance/kernel-journal-20261117.bDb45KPF`. The kernel was
untainted, no units had failed and no tasks were in uninterruptible sleep. The
complete private-metrics verifier passed again, including actual web mode `none`
and no 19999 listener, claimed/online Cloud connectivity, all three loopback
sockets, trusted PostgreSQL TLS and restricted monitoring role, Redis ACL denials,
all four collectors with repeated numeric samples and all five public services'
request/latency series. The installed snapshot fingerprint was unchanged.

The temporary Go SQLite diagnostic used the host's existing SQLite library in
read-only mode. Its disposable fixture hash was unchanged before/after the test.
The real agent metadata recorded `system_post_update_reboot_status` **Clear at
12:29:29 UTC**, value zero, still using the stock `reboot.conf` rule. Chinwag's
rollback alert was **Clear at 12:30:04 UTC**, value zero, using the same
`C78866D0669D413B982C1468435C1B40` custom configuration hash, `database=chinwag`
selector, 10%/5% hysteresis and 300-second up/down delays. All five other database
rollback alerts recorded Clear at the same time using the stock 2%/0% rule.
These transitions followed this boot's Netdata restart and establish fresh
agent evaluation. Cloud's earlier match-count discrepancy was not used to
change the working rule.

Cloud displayed fresh PostgreSQL connections data at **12:37:08 UTC**, Redis
connections/ping data at **12:38:08 UTC**, Prometheus service request/latency
series at **12:38:56 UTC**, and native Traefik requests/latency at **12:39:52 UTC**,
all on the same live node. Its Raised view reported **no active alerts**, last
updated **12:40:28 UTC**: the recent-reboot warning also cleared naturally.
All five external endpoint and eight private-port checks passed again at
12:39:43, with no failed units, stuck tasks or kernel taint at the accompanying
host check.

At **12:44:33 UTC**, uptime was **923.64 seconds**, exceeding the planned
15-minute observation period. All ten services were still 1/1; all containers
with probes were healthy, with zero automatic restarts and no OOM kills. The
external inventory/keyword and private-port checks passed again at 12:44:29.
There were no failed units, uninterruptible tasks, kernel taint or pending-reboot
marker. No new application or collector failure required intervention during
observation. The fixed-kernel boot was accepted before persistent-default
promotion.

At **12:52:19 UTC**, the owner completed the administrator promotion command.
It checked the same boot ID, more than 15 minutes of uptime, the current kernel
log, host/service state, idle package maintenance and unchanged infrastructure
fingerprint before changing the default. The private rollback copy is
`/var/backups/perry-maintenance/kernel-default-20261117.p3g2UKYZ`. It marked
`linux-image-6.1.0-54-cloud-amd64` manually installed, restored `GRUB_DEFAULT=0`,
regenerated GRUB, passed `grub-script-check`, and verified that normal entry 0
loads the 56 kernel **and its matching initramfs**. Both 54 boot entries and boot
files remain available. The command would restore its prior boot files if
generation or validation failed; that rollback was not needed. The reviewed
wrappers passed shell syntax checks and ShellCheck, and the entry validator
accepted the real configuration while rejecting mismatched-kernel and
mismatched-initramfs fixtures.

Independent SSH verification at **12:52:51 UTC** confirmed `GRUB_DEFAULT=0`, the
generated 56 default, valid GRUB syntax, empty one-time entry, manually retained
54, both installed package versions and unchanged snapshot fingerprint. The host
still ran 56 with boot ID `08f78547-64c2-4c8a-bc3d-7fae8e9e0fc6`; no additional
reboot was needed. The accompanying public and private-port checks passed.
The fixed-kernel acceptance checkbox is now closed on actual production
evidence.

For a later regression, keep fresh provider-console access ready and select the
retained 54 kernel through the administrator bootloader path. To persist that
rollback, set the explicit 54 entry recorded above, regenerate GRUB and validate
it before a scheduled reboot. Allow the same host-wide application outage during
rollback and repeat application/monitoring acceptance afterward. The provider's
zero-second menu timeout remains in effect; use its official rescue DVD if menu
selection is unavailable. Review the dated private boot copies before restoring
any file over later administrator changes. The separate monitoring configuration
archive and Netdata Docker-group follow-up are unchanged by this kernel work.

The subsequently authorized Docker-group hardening was completed separately;
see the [17 November Docker access record](netdata-docker-access-20261117.md).
