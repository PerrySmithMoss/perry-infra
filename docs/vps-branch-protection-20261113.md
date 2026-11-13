# VPS branch protection verification — 2026-11-13

Verification is in progress. The task stays open until the effective rules,
enforcement probes, reviewed PRs and real protected deployments pass for all six
repositories.

## Policy and initial audit

All repositories belong to the personal `PerrySmithMoss` account. The audit found
no repository or inherited rulesets. Five default branches already enforced the
baseline; Im Listening's `master` was unprotected. Existing policies and
production environment configuration are backed up outside the checkout.

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

## Verification evidence

Pending: fresh PR check contexts, effective-policy readback, isolated negative
enforcement probes, green PR merges, production releases and final VPS health.

Enforcement probes must target disposable branches, never the live release
branches. Any deliberately failed fixture status is negative-test evidence,
not a failure of the normal application CI or production deployment.

