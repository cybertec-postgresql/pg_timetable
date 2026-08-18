# Samples

## Basic

This sample demonstrates how to create a basic one-step chain with parameters. It uses CTE to directly update the
**timetable** schema tables.

```sql
--8<-- "samples/Basic.sql"
```

## Send email

This sample demonstrates how to create an advanced email job. It will check if there are emails to send,
will send them and log the status of the command execution. You don't need to setup anything, every parameter
can be specified during the chain creation.

```sql
--8<-- "samples/Mail.sql"
```

## Download, Transform and Import

This sample demonstrates how to create enhanced three-step chain with parameters. It uses DO statement to directly update the
**timetable** schema tables.

```sql
--8<-- "samples/Download.sql"
```

## Run tasks in autonomous transaction

This sample demonstrates how to run special tasks out of chain transaction context. This is useful for special routines and/or 
non-transactional operations, e.g. *CREATE DATABASE*, *REINDEX*, *VACUUM*, *CREATE TABLESPACE*, etc.

```sql
--8<-- "samples/Autonomous.sql"
```

## Shutdown the scheduler and terminate the session

This sample demonstrates how to shutdown the scheduler using special built-in task. This can be used to control 
maintenance windows, to restart the scheduler for update purposes, or to stop session before the database should be 
dropped.

```sql
--8<-- "samples/Shutdown.sql"
```

## Access previous task result code and output from the next task

This sample demonstrates how to check the result code and output of a previous task. If the last task failed, 
that is possible only if *ignore_error boolean = true* is set for that task. Otherwise, a scheduler will 
stop the chain. This sample shows how to calculate failed, successful, and the total number of tasks executed. 
Based on these values, we can calculate the success ratio.

```sql
--8<-- "samples/ManyTasks.sql"
```

## Secrets

`samples/Mail.sql` and `samples/RemoteDB.sql` demonstrate the secret store:
a `${secret:name}` reference in a parameter (jsonb) or a `database_connection`
conninfo string is replaced at execution time with the decrypted value of the
matching `timetable.secret` row for the running client.

The store is **write-only by design** — values are encrypted at rest with
`pgcrypto.pgp_sym_encrypt`, decrypted only by the `SECURITY DEFINER` function
`timetable.resolve_secret()`, and never exposed back to SQL as plaintext
outside the resolved parameter. There is no surrogate `secret_id`; rows are
addressed by `(client_name, secret_name)` only.

### `pgcrypto` is an optional prerequisite

`pgcrypto` is **not** installed or required by pg_timetable itself. pg_timetable
never issues `CREATE EXTENSION`, never probes for the extension outside the
body of `timetable.resolve_secret()`, and runs normally on a database without
`pgcrypto` — the secret store simply becomes unavailable. Installing
`pgcrypto` is the responsibility of whoever deploys the database:

- Since PostgreSQL 13, `pgcrypto` is a **trusted** extension and can be
  installed by any role holding `CREATE` on the database, so managed services
  (RDS, Azure, Cloud SQL, Supabase and similar) need no superuser.
- On PostgreSQL 12 and older, superuser or an allowlist entry is required.
- The PostgreSQL build must include OpenSSL; on a build without it the
  extension cannot be installed and the secret store is silently unavailable
  (everything else is unaffected).
- The extension may live in any schema. `resolve_secret` is `LANGUAGE plpgsql`
  and discovers `pgcrypto` at call time via `pg_catalog.pg_extension` /
  `pg_catalog.pg_namespace`; the schema is interpolated into the decrypt
  query as dynamic SQL. The migration does not prescribe an installation
  schema.

### Trust boundary (honest)

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

### Use the feature with your own chains

1. Configure `--secret-key` (or `PGTT_SECRET_KEY`) on the scheduler process.
2. Install `pgcrypto` (any schema) and insert a row into `timetable.secret`
   with `pgp_sym_encrypt` using the same key:
   ```sql
   CREATE EXTENSION IF NOT EXISTS pgcrypto; -- by the DBA, not by pg_timetable
   INSERT INTO timetable.secret (client_name, secret_name, value_enc)
   VALUES ('worker-1', 'smtp_main',
           pgp_sym_encrypt('your-password', 'PGTT_SECRET_KEY_VALUE'));
   ```
   The cluster role must own `timetable.secret` for this to succeed.
3. Replace the literal in your parameter with `"${secret:your_name}"` (for
   jsonb fields) or `password=${secret:your_name}` (for connection strings).
4. Optional: grant a separate administrative role write-only access. The
   schema grants no default privileges on `timetable.secret`, so an operator
   who wants to delegate secret administration without revealing plaintext
   must `GRANT INSERT, UPDATE, DELETE ON timetable.secret TO admin_role`
   manually:
   ```sql
   GRANT INSERT, UPDATE, DELETE ON timetable.secret TO admin_role;
   ```
   `resolve_secret` is owned by the scheduler role and not granted to any
   other role by default.

### PROGRAM-tasks (argv exposure)

PROGRAM-tasks (`samples/Shell.sql`) take a JSON-encoded argv array. Resolved
values land in argv, which is observable via `/proc/<pid>/cmdline` and
`/proc/<pid>/environ` on the worker host. This is a documented trade-off of
v1, not a bug. Prefer environment variables or stdin for sensitive argv in
production chains; passing the literal `${secret:x}` to a child process
would be silently wrong rather than loudly unsupported.

### Debug-level logging

At `--log-level=debug` and `--log-database-level=debug`, the pgx tracer
persists query entries to `timetable.log`. The pgx logger is context-aware:
queries executed under `log.WithoutQueryArgs(ctx)` (the marker used by
`ResolveSecrets*`) drop the `args` field while retaining `sql`, so neither
the plaintext nor the encryption key reaches `timetable.log` or the
stdout/file log.

`execution_log.params` is intentionally the unresolved `${secret:name}` form.

### Guidance

- Prefer `.pgpass` / `.pg_service.conf` on the worker host over `${secret:...}`
  for remote Postgres passwords. The secret store exists for credentials that
  have no host-local equivalent, such as SMTP.
- The migration does not install `pgcrypto`; the schema applies cleanly to a
  database where the extension is absent and the connection role could not
  install it. A scheduler starts with no error and no warning in that case,
  and `secret_count()` returns `0`.
