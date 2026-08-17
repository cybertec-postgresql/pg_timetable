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
matching `timetable.secret` row for the running client. The store is
**write-only by design** — values are encrypted at rest with
`pgcrypto.pgp_sym_encrypt`, decrypted only by the `SECURITY DEFINER` function
`timetable.resolve_secret`, and never exposed back to SQL as plaintext outside
the resolved parameter.

Trust boundary: the running worker is fully trusted. Secrets protect against
DB readers, backups, dumps, and audit-log spill — not against a compromised
worker host. See the Secrets section and
[`docs/database_schema.md`](database_schema.md) for the full masking rules.

To use the feature with your own chains:

1. Configure `--secret-key` (or `PGTT_SECRET_KEY`) on the scheduler process.
2. Insert a row into `timetable.secret` with `pgp_sym_encrypt` using the same
   key. The cluster role must own `timetable.secret` for this to succeed.
3. Replace the literal in your parameter with `"${secret:your_name}"` (for
   jsonb fields) or `password=${secret:your_name}` (for connection strings).
4. If you want a separate administrative role to be able to manage secrets
   without being able to read plaintext, `GRANT SELECT (client_name, secret_name)
   ON timetable.secret TO admin_role` — `resolve_secret` is owned by the
   scheduler role and is not granted to anyone by default.

PROGRAM-tasks (`samples/Shell.sql`) take a JSON-encoded argv array. Resolved
values land in argv, which is observable via `/proc/<pid>/cmdline` and
`/proc/<pid>/environ` on the worker host — this is a documented trade-off, not
a bug. Prefer env vars or stdin for sensitive argv in production chains.

Debug-level logging of `execution_log.params` is intentionally the unresolved
`${secret:name}` form. The pgx logger drops `args` for queries that carry a
resolved secret into `resolve_secret(...)` itself, so the plaintext never
appears in trace logs.