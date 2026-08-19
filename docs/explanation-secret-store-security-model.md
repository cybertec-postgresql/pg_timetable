# Secret Store Security Model

The secret store is **write-only by design** — there is no plaintext read path; values are encrypted at rest with `pgcrypto.pgp_sym_encrypt`, decrypted only by the `SECURITY DEFINER` function `timetable.resolve_secret()` when the caller supplies the encryption key, and never exposed back to SQL as plaintext outside the resolved parameter. There is no surrogate `secret_id`; rows are addressed by `(client_name, secret_name)` only.

The running scheduler process is the trusted execution boundary. The secret
store raises the bar against:

- other database roles (Grafana, reporting, ad-hoc DBA sessions),
- logical-replica subscribers, and
- `pg_dump` archives taken without the encryption key.

It does **not** raise the bar against a compromised worker host, `ps` /
auditd argv inspection, or any party that holds both `value_enc` and the
`PGTT_SECRET_KEY` value. The scheduler's connection role can read
`value_enc` directly; confidentiality rests on possession of the encryption
key, which the database never stores.

This feature raises the bar against other database roles, logical replicas,
and `pg_dump` without the key. It does not by itself satisfy any specific
regulatory secret-management control (PCI-DSS key custody, SOC 2 rotation).

PROGRAM-tasks (`samples/Shell.sql`) take a JSON-encoded argv array. Resolved
values land in argv, which is observable via `/proc/<pid>/cmdline` and
`/proc/<pid>/environ` on the worker host. This is a documented trade-off of
v1, not a bug.

At `--log-level=debug` and `--log-database-level=debug`, the pgx tracer
persists query entries to `timetable.log`. The pgx logger is context-aware:
queries executed under `log.WithoutQueryArgs(ctx)` (the marker used by
`ResolveSecrets*`) drop the `args` field while retaining `sql`, so neither
the plaintext nor the encryption key reaches `timetable.log` or the
stdout/file log.

`execution_log.params` is intentionally the unresolved `${secret:name}` form.

## Trade-offs Against Alternatives

`.pgpass` / `.pg_service.conf` on the worker host is a better fit than
`${secret:...}` for remote Postgres passwords: it keeps the credential off
the database entirely. The secret store earns its complexity for
credentials that have no host-local equivalent, such as SMTP passwords.

Environment variables or stdin are a better fit than `${secret:...}` for
sensitive `PROGRAM`-task argv in production: passing the literal
`${secret:x}` to a child process would be silently wrong (the placeholder
string itself, not the secret) rather than loudly unsupported.
