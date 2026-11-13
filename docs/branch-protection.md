# Default branch protection

The VPS release source is `main`. Its required policy is:

- Changes enter through pull requests, including changes made by administrators.
- Required checks: `scan`. The branch must be up to date.
- GitHub Actions checks are bound to the GitHub Actions app (ID 15368).
  The existing Vercel commit status uses its legacy context.
- Force-pushes and branch deletion are blocked; conversations must be resolved.
- No PR bypass actor is configured. The owner-operated baseline requires a PR
  and passing checks, with zero additional reviewer approvals.

The `production` environment accepts only `main`. Releases continue through
the existing pinned SSH transport and restricted VPS broker; branch protection
does not grant a workflow permission to install host configuration or change
application network memberships.

The [central production verification record](https://github.com/PerrySmithMoss/perry-infra/blob/main/docs/vps-branch-protection-20261113.md)
tracks the effective settings, disposable enforcement checks, reviewed PR and
production deployment for all six VPS repositories. Keep required check names
in sync with actual PR jobs when changing workflows. Retain stronger existing
rules and verify the effective API settings after any policy change.

