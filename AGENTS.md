# Instructions for coding agents

## Infrastructure tooling

- Use Go for new infrastructure utilities, deployment automation, credential
  tooling, and their tests. Prefer the standard library and the Go version used
  by the existing deployment brokers. Do not introduce Python scripts.
- Keep shell scripts to small wrappers and straightforward command orchestration.
  Use JavaScript only where a check needs to exercise an application's actual
  JavaScript libraries. Existing tooling in other languages can remain unless
  the requested work requires changing it.
- Follow the administrator-installed snapshot and restricted release-broker
  boundaries. Include new runtime configuration in snapshot fingerprints and
  installation steps. Never use Docker access to bypass required host privileges.
- Never commit credentials, private keys, session data, or backups. Generate and
  handle production secrets outside the checkout and do not print their values.
- Preserve existing data and unrelated local changes. For migration tasks,
  document downtime and rollback, and leave task checkboxes open until the
  production verification described in `docs/tasks.md` has actually passed.
