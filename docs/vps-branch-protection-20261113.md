# VPS branch protection verification — 2026-11-13

Completed in production on 2026-11-13. All six effective policies, enforcement
fixtures, reviewed PRs and protected release workflows passed. Final live
checks confirmed all ten services 1/1 and all five public health endpoints
returning HTTP 200. The task is closed.

## Policy and initial audit

All repositories belong to the personal `PerrySmithMoss` account. The audit found
no repository or inherited rulesets. Five default branches already enforced the
baseline; Im Listening's `master` was unprotected. Existing policies and
production environment configuration are backed up outside the checkout.

Im Listening now requires `verify` from GitHub Actions and the existing Vercel
status, with administrator enforcement, strict checks, mandatory PRs, resolved
conversations and blocked force-pushes/deletion. The other five policies are
preserved. All six production environment policies were independently compared
with their initial audit and still permit only the exact branch in the table.

The baseline requires a PR, up-to-date required checks, administrator enforcement,
resolved conversations, and blocked force-pushes/deletion. It retains zero
additional reviewer approvals for the owner-operated repositories. No bypass
actor is added and no existing required check is removed. GitHub Actions checks
are bound to app ID 15368; existing Vercel legacy commit-status contexts remain.

| Repository | Default / VPS release branch | Required checks | Production environment |
| --- | --- | --- | --- |
| perry-infra | main | scan | production: main only |
| BulkBets | master | validate, e2e, Vercel | production: master only |
| chinwag | master | run-tests, build-client, build-api-image, Vercel | VPS Production: master only |
| logistics-realtime-dashboard | main | validate, e2e, build-api-image, Vercel | VPS Production: main only |
| im-listening | master | verify, Vercel | VPS Production: master only |
| VitaTrack | master | build-client, build-api-image, Vercel | VPS Production: master only |

All existing production branch restrictions and the infrastructure environment's
owner approval are retained. The infrastructure runtime fingerprint remains
`b299443ef76b27a11c99964b3aede0322a64e96c5bfb7add8416de2747cf51d5`;
no administrator snapshot installation or release-broker permission change is
part of this task.

BulkBets's separate `production` ref remains the existing Vercel promotion
target, updated by the tested `master` workflow after its VPS deployment passes.
The protected VPS release source is `master`; the promotion job receives no
bypass of its protection.

## Enforcement probes

All six disposable fixtures passed. Each copied its live PR, administrator,
strict-check, conversation, force-push and deletion settings, substituting one
controlled legacy status context for the production check list. Existing
workflows were removed only from the disposable base trees so synthetic failures
did not run application CI or production release jobs. Production check names
and their GitHub Actions bindings were independently verified on the real PRs.

For every fixture, the owner account's failed-check merge was rejected, and a
separate commit with a passing status but no PR was rejected as a direct update.
Force-push and protected-branch deletion were also rejected. GitHub reported
`Required status check ... is failing` and `GH006` with the applicable PR,
force-push or deletion reason. Each live default ref stayed unchanged throughout
the probes. A green fixture PR then merged as the positive control; all temporary
refs and their protection rules were removed, and the live policy readback was
byte-for-byte unchanged from its post-application audit.

A fast-forward that completes an existing green PR is a valid merge path:
GitHub accepted that positive control for the infrastructure fixture and marked
its PR merged. The rejected direct-update probe uses a different commit with no
PR, even though its controlled status passes. No claim is made that GitHub
forbids every Git transport operation that completes a compliant PR.

| Repository | Disposable fixture | Result |
| --- | --- | --- |
| perry-infra | [#29](https://github.com/PerrySmithMoss/perry-infra/pull/29) | All four rejected; positive control passed; refs removed |
| BulkBets | [#33](https://github.com/PerrySmithMoss/BulkBets/pull/33) | All four rejected; positive control passed; refs removed |
| chinwag | [#10](https://github.com/PerrySmithMoss/chinwag/pull/10) | All four rejected; positive control passed; refs removed |
| logistics-realtime-dashboard | [#10](https://github.com/PerrySmithMoss/logistics-realtime-dashboard/pull/10) | All four rejected; positive control passed; refs removed |
| im-listening | [#20](https://github.com/PerrySmithMoss/im-listening/pull/20) | All four rejected; positive control passed; refs removed |
| VitaTrack | [#12](https://github.com/PerrySmithMoss/VitaTrack/pull/12) | All four rejected; positive control passed; refs removed |

Raw policy backups, probe logs and progress inputs remain outside Git under the
private local `branch-protection-20261113` audit directory. No credentials,
private keys, sessions or backups are committed.

## Reviewed PRs and protected releases

All six real PRs merged with their required checks passing, and all six protected
production workflows completed successfully. Final live checks also passed.

| Repository | Merged PR | Required PR CI | Protected production run | Merged revision |
| --- | --- | --- | --- | --- |
| perry-infra | [#28](https://github.com/PerrySmithMoss/perry-infra/pull/28) | [passed](https://github.com/PerrySmithMoss/perry-infra/actions/runs/43420269094) | [passed](https://github.com/PerrySmithMoss/perry-infra/actions/runs/43423233005) | `7fa645dffdf10b0049f1a78f50202381b38ab553` |
| BulkBets | [#32](https://github.com/PerrySmithMoss/BulkBets/pull/32) | [passed](https://github.com/PerrySmithMoss/BulkBets/actions/runs/43421345216) | [passed](https://github.com/PerrySmithMoss/BulkBets/actions/runs/43423133538) | `bd89cac88355df4eecd1062bb58c2fd52fdd4c10` |
| chinwag | [#9](https://github.com/PerrySmithMoss/chinwag/pull/9) | [passed](https://github.com/PerrySmithMoss/chinwag/actions/runs/43420203670) | [passed](https://github.com/PerrySmithMoss/chinwag/actions/runs/43423237157) | `1bf3836bbcdde2a8f0c639aca4975a4d36ff7cd6` |
| logistics-realtime-dashboard | [#9](https://github.com/PerrySmithMoss/logistics-realtime-dashboard/pull/9) | [passed](https://github.com/PerrySmithMoss/logistics-realtime-dashboard/actions/runs/43420206649) | [passed](https://github.com/PerrySmithMoss/logistics-realtime-dashboard/actions/runs/43423240683) | `55fe4fd555ed742b6984d3acb581a729fe4d288a` |
| im-listening | [#19](https://github.com/PerrySmithMoss/im-listening/pull/19) | [passed](https://github.com/PerrySmithMoss/im-listening/actions/runs/43420211606) | [passed](https://github.com/PerrySmithMoss/im-listening/actions/runs/43423159118) | `b6fc86fac6d3096e1394cec392a24512c8d7e76f` |
| VitaTrack | [#11](https://github.com/PerrySmithMoss/VitaTrack/pull/11) | [passed](https://github.com/PerrySmithMoss/VitaTrack/actions/runs/43420216715) | [passed](https://github.com/PerrySmithMoss/VitaTrack/actions/runs/43423244306) | `35d84306f490055637825a66a5af17f2c5b3355d` |

Docs-only push filters are retained. Chinwag and VitaTrack used their normal
manual default-branch workflows; Fleet's manual workflow forced an API build
for this verification after its docs-only push checks. Infrastructure used its
normal manual workflow and retained its required owner production approval.
BulkBets and Im Listening used their automatic default-branch production runs.
All releases use the existing pinned SSH transport and restricted brokers.

## BulkBets security-gate repair

The first fresh BulkBets PR run exposed `GO-2026-6859` in the existing Go 1.27.3
standard library. The required vulnerability check correctly blocked the PR.
The reviewed fix raises the server minimum and digest-pinned builder to Go
1.27.4, which includes the [Go security fixes](https://go.dev/doc/devel/release#go1.27.4).
Go 1.27.4 race tests passed and `govulncheck` reported no vulnerabilities;
the full PR validation, browser/storage tests and Vercel preview then passed.
No security check was removed or weakened. The app has no schema changes and
its release broker permissions are unchanged.

## Final production verification

At 12:30 UTC on 2026-11-13, all ten production services were 1/1 and all five
public API health endpoints returned HTTP 200. Independent Docker inspection
confirmed that each app had exactly one running container, that its actual image
matched the service's requested image, and that its OCI revision matched the
reviewed default-branch merge above.

| Application repository | Running immutable image | Verified OCI revision |
| --- | --- | --- |
| BulkBets | `ghcr.io/perrysmithmoss/bulkbets@sha256:80451c5c1a1d13c0297277f7cdd4a0c3f708edf453f375273b37b1fadec0e321` | `bd89cac88355df4eecd1062bb58c2fd52fdd4c10` |
| chinwag | `ghcr.io/perrysmithmoss/chinwag@sha256:ed812ac12e6f20c532677c59437255abcc3931a7a0283ad65838ea38dd673d07` | `1bf3836bbcdde2a8f0c639aca4975a4d36ff7cd6` |
| logistics-realtime-dashboard | `ghcr.io/perrysmithmoss/logistics-realtime-dashboard@sha256:21a01b89fff3897322daa4eb622bd5fe3b4ab8a2ca8548293e16091e6d125e23` | `55fe4fd555ed742b6984d3acb581a729fe4d288a` |
| im-listening | `ghcr.io/perrysmithmoss/im-listening-api@sha256:dbd8786e1dfa247ba18207018010c2dbf6f22e3d571b9e3b1b96113633b2254d` | `b6fc86fac6d3096e1394cec392a24512c8d7e76f` |
| VitaTrack | `ghcr.io/perrysmithmoss/vita-track@sha256:e3099fde3a04c871f8af544b7ad6a7bd7113e91b2ce61762c2d7db3bdf19f627` | `35d84306f490055637825a66a5af17f2c5b3355d` |

All five Vercel statuses on the merged revisions were successful; Im Listening's
separate API/migrations status was also successful. BulkBets's `production`
promotion ref equalled its reviewed merge `bd89cac88355df4eecd1062bb58c2fd52fdd4c10`.
The existing frontend promotion path and protected VPS releases both worked.

Final API readback found the same six enforced policies, no ruleset drift and no
disposable fixture refs. The exact default-branch production environment policies
and infrastructure owner approval were preserved. No bypass actors, broader
SSH privileges or host snapshot changes were introduced. The branch-protection
task is closed; Fleet's separate provider-quota follow-up remains open.

