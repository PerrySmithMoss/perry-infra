# VPS monitoring: Netdata Community, Cloud-only access

This is administrator-managed **host configuration**, outside the Swarm stack.
The existing `infra-release` broker/key is not allowed to install host packages.
Do not broaden that account, the Docker socket proxy ACL, application overlays,
or database exposure to deploy monitoring. No monitoring port or Traefik router
is added. The repository is public: never commit claim.conf, tokens, Cloud keys,
SMTP passwords, database credentials, or backups.

## Deployment status and prerequisites

Files in Git do not prove live installation, Cloud claiming, email delivery, or
UptimeRobot configuration. Record completion only after verifying each below.

- Debian 12, systemd, administrator SSH with sudo; about 500 MiB available RAM
  as a starting allowance, then measure real peak usage.
- IONOS firewall must not publicly allow TCP 19999, including via broad IPv4/IPv6
  rules. Preserve existing SSH, app and required Swarm rules. Do not replace the
  host firewall or enable UFW blindly on a Docker host.
- Outbound DNS, HTTPS/WSS 443 and package repository access must work.
- Create a private Netdata Cloud Space/Room with only the owner as a member.
  Use Google/GitHub OAuth with MFA at that provider. Community covers this one
  host and its containers; paid trial features are not required.

## Install (administrator terminal on the VPS)

Use a reviewed checkout of this repository, then run from its root:

```sh
sudo sh monitoring/install.sh
```

This installs the official native Debian package on the stable channel, enables
stable automatic updates, and opts out of anonymous agent telemetry. It fetches
and executes Netdata's official HTTPS kickstart installer as root: the vendor
installer/package repositories are a trust dependency, not a vendored or pinned
release. A copy of the fetched installer and its SHA256 are saved with the backup.

The managed config and systemd startup guard are installed **before** the package
can start Netdata. The guard refuses startup unless `[web] mode = none` is present
exactly once. Package-config drift therefore fails closed. The IONOS firewall is
still required as a separate protection during installation and upgrades.

A separately managed existing Netdata installation is refused rather than silently
overwritten. Subsequent runs can reapply this repository's managed configuration.
They back up the prior config/guard in root-only
`/var/backups/perry-monitoring/install.*`. Failures after configuration changes
stop Netdata; they do not redeploy application containers. The installer is not a
transactional package rollback; inspect a failed install before retrying.

Resource choices for the 2 GB VPS:

- Five-second collection instead of one second.
- ML training disabled; ordinary health alerts remain enabled.
- Two persistent tiers: 512 MiB/7 days at native resolution, 256 MiB/3 months
  at five-minute resolution. The earlier size/time bound wins. These are soft
  metric-storage targets; metadata and temporary overshoot add disk usage.
- No hard MemoryMax or CPU quota initially: do not hide an undersized collector
  by making it repeatedly crash. Check consumption during a real busy period.

## Link the agent without exposing secrets

In Netdata Cloud, Add nodes -> Linux gives the Space token and Room ID(s).
Do not run its default installer command over this managed setup. In your own
administrator SSH terminal, from the repository root:

```sh
sudo bash monitoring/claim.sh
```

Paste values into its prompts. The token input is hidden and not passed through
argv, the environment, shell history, CI or chat. The script writes
`/etc/netdata/claim.conf` as root:netdata 0640 with TLS verification enabled.
It refuses to reassign an already-claimed agent or overwrite an existing claim
file. If claiming fails, diagnose the existing claim instead of deleting identity
files or repeatedly generating new agent identities.

Then wait for Cloud to show the node online and run:

```sh
sudo sh monitoring/verify.sh
```

Before claiming, `sudo sh monitoring/verify.sh --local-only` checks local safety.
Verification waits up to 30 seconds for the agent control pipe after startup.
Full verification requires claimed/online status, checks the **running** config
and no TCP listener on 19999, and reports systemd memory use including collectors.
It deliberately prints no claim token. Do not paste entire cloud.d/claim.conf files.

From a different machine:

```sh
curl --connect-timeout 5 http://194.164.126.180:19999/
```

Expected: connection refused or timeout. Also test the VPS public IPv6 address if
it has one. Dashboard access should work only through https://app.netdata.cloud/.
Test on mobile data to confirm remote access independently of the home network.

Check host CPU/memory/disk charts plus each of the nine Swarm containers, including
CPU throttling. Container churn after deployment is expected. Resource charts do
not establish desired-vs-running Swarm replicas. Over 24-48 hours, compare:

```sh
free -h
sudo systemctl show netdata -p MemoryCurrent -p TasksCurrent
df -h /
```

Cloud is a remote access/control service here, not an independent archive. Metric
history remains on the VPS; it may be unavailable when the VPS is down. Restrict
Space membership and remember the Cloud service can handle displayed metric data.

## Personal email alerts (no VPS SMTP)

In Cloud profile -> Settings -> Notifications, enable personal Email, join/select
the VPS Room, and enable Critical, Warning, Clear, Unreachable and Reachable.
Space settings -> Alerts & Notifications must also allow personal email. Use
personal Email, not the paid Group Email integration. Confirm the settings remain
available on Community after any Business trial ends.

Start with stock alerts; inspect which ones are active rather than assuming a
particular disk/CPU rule exists. Suggested tuning after observing the workload:
80%/90% disk thresholds, available RAM below ~200 MiB for several minutes, host CPU
above ~85% for ten minutes, container-health issues and API failures. Those are
starting policy suggestions, not custom rules installed by these scripts. App
health alerts depend on the app having a meaningful Docker healthcheck.

Test email delivery without stopping apps: stop **only Netdata**, wait for its
Unreachable email, then start it and verify the recovery message. Allow several
minutes, and restart it even if no mail arrives:

```sh
sudo systemctl stop netdata
# Wait for the Cloud status change/email; leave apps running.
sudo systemctl start netdata
sudo sh monitoring/verify.sh
```

Do not claim success from a green notification toggle alone. Check spam and Room
membership if delivery fails. Stock alert noise can be tuned after a baseline;
do not disable all warnings as a shortcut.

## Independent external checks

Use UptimeRobot Free: five-minute HTTP(S) checks, verified email down/up alerts,
and no public status page needed. Enable account MFA if offered. Netdata port
19999, PostgreSQL and Redis must remain private.

`endpoints.tsv` is the inventory, **not** an automatic provisioning job. It lists
the public endpoints and expected request methods for all five APIs. Use each
app-served health endpoint through Traefik, not a frontend/login page or shared
proxy response. Verify status, response content and redirect behaviour before
creating a monitor. Health responses must contain no internal diagnostics.

For each URL, create a monitor with 300-second interval and owner email alerts.
The Free UI currently uses HEAD for ordinary HTTP monitors; custom methods and
status codes are paid. Im Listening, Fleet, Chinwag and Vita Track support HEAD.
For BulkBets use a **keyword** monitor requiring `A-OK!` in the GET response:
its GET succeeds but HEAD returns 405. Verify every monitor reports Up.

Chinwag and Vita Track expose `/healthz` as lightweight process-liveness routes:
GET returns 200 with `{ "status": "ok" }`, HEAD returns 200 without a body, and
responses use `Cache-Control: no-store`. They require no cookies or GraphQL
headers and perform no database query. They do not prove ongoing database
availability. Their existing Docker probes remain intact, as do application
authentication and GraphQL CSRF protection. Add separate readiness checks if you
need continuous dependency verification, with short timeouts and cheap queries.

Confirm actual receipt of a test notification; a green monitor or attached email
contact alone does not verify mailbox delivery.
For an end-to-end down/up test, use a temporary test monitor: start with a healthy
URL, change it to a verified failing URL, receive the down email, restore the
healthy URL, and receive recovery. Do not stop production services for this test.

## Deeper service metrics (separate follow-up)

Host/container CPU/RAM metrics work without app code changes. Do not assume that
means request latency, Go goroutines, Node event-loop lag, PostgreSQL internals,
or Redis evictions are collected.

- PostgreSQL is reachable only on private overlays. Plan a narrowly scoped
  collector connection and dedicated non-superuser monitoring role (`pg_monitor`
  plus only the access the chosen collector needs), with TLS as appropriate.
  Do not use the shared admin password or publish port 5432. Table/index metrics
  may require extra privileges/connections; review before enabling.
- Shared Redis is owned by perry-infra; follow the [migration runbook](../redis/README.md)
  and verify the production cutover before changing collection. Configure a separate
  collector ACL with only required commands and an administrator-reviewed private
  connection path. Do not reuse application/admin credentials or expose 6379 publicly.
- Traefik currently has no metrics endpoint. A later change can enable a dedicated
  private metrics entrypoint and collector path, reviewed with the Swarm snapshot.
  Do not expose the API/dashboard or put it on the public web entrypoints.
- The existing HAProxy proxy intentionally denies `/containers` requests. Keep it
  restricted for Traefik. Netdata uses host cgroups for container resources; any
  extra Docker API access needed for names/health must be evaluated separately.
  Do not add netdata to the Docker group as a quick fix (root-equivalent access).

## Maintenance and rollback

Check stable update status per Netdata's updater documentation. This host package
is not covered by the repo's container-image Trivy scans. The CI monitoring checks
validate scripts/configuration only; they neither install on the VPS nor touch
Cloud accounts. monitoring/** is not added to the Swarm deployment trigger.

To revert a configuration change, stop Netdata, restore the prior `netdata.conf`,
startup guard and drop-in from the printed root-only backup, run
`systemctl daemon-reload`, then start Netdata and rerun verification. Never restore
an old public-dashboard config while retaining the Cloud-only expectation. Inspect
backup contents first: first-install backups have no previous files to restore.

To disable a first installation safely:

```sh
sudo systemctl disable --now netdata.service
```

Keep public 19999 blocked. Leave its configuration/guard and data in place until
you choose whether to reinstall, remove packages, or archive local history. Stop
Netdata before handling its data. Cloud identity files and claim.conf are secrets:
back them up only to encrypted/private storage; do not attach them to issues.

The existing infrastructure broker, SSH policy, Swarm manifests, networking and
PostgreSQL data volume are unchanged by these scripts.

## Sources

- https://learn.netdata.cloud/docs/netdata-agent/installation/linux/
- https://learn.netdata.cloud/docs/netdata-cloud/connect-agent
- https://learn.netdata.cloud/docs/netdata-agent/configuration/securing-agents/
- https://learn.netdata.cloud/docs/netdata-agent/configuration/database
- https://learn.netdata.cloud/docs/alerts-&-notifications/notifications/centralized-cloud-notifications/manage-notification-methods
- https://help.uptimerobot.com/en/articles/11604710-who-should-use-uptimerobot-s-free-plan
