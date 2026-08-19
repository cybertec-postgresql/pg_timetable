# Use the Secret Store

## Store and reference a secret

`samples/Mail.sql` and `samples/RemoteDB.sql` demonstrate the secret store:
a `${secret:name}` reference in a parameter (jsonb) or a `database_connection`
conninfo string is replaced at execution time with the decrypted value of the
matching `timetable.secret` row for the running client.

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

## Use secrets with YAML-authored chains

YAML-authored chains do **not** support `${secret:name}` references in v1.
The reference syntax is a string-substitution feature implemented in the
Go runtime after `parameter.value` is materialized into the database; the
YAML loader does not perform secret resolution. A YAML chain that needs a
secret must either:

- reference a `timetable.task` row whose `parameter.value` already contains
  `${secret:name}` (i.e., the chain was originally created via SQL using
  `samples/Mail.sql` or `samples/RemoteDB.sql` as a template; see the
  [Store and reference a secret](#store-and-reference-a-secret) section above
  for the steps), or
- use a connection-string literal in `database_connection` and accept the
  trade-off (the password is then visible to DB readers, backups, and dumps).

### What you need to enable the secret store

`pgcrypto` is **not** installed by pg_timetable. To use `${secret:name}`
references, the database administrator must install `pgcrypto` once per
database (any schema; since PostgreSQL 13 it is a **trusted** extension
and can be installed by any role holding `CREATE` on the database). The
scheduler is configured with `--secret-key` (or `PGTT_SECRET_KEY`) and
secret rows are inserted with `pgp_sym_encrypt` from the same schema.

The store is opt-in syntax: chains whose `parameter.value` holds a literal
password continue to work unchanged.

For the schema and reference syntax, see [Secret Store Reference](secret_store.md).

## Recommendations

- Prefer `.pgpass` / `.pg_service.conf` on the worker host over `${secret:...}`
  for remote Postgres passwords. The secret store exists for credentials that
  have no host-local equivalent, such as SMTP.
- Prefer environment variables or stdin for sensitive argv in production
  chains; passing the literal `${secret:x}` to a child process would be
  silently wrong rather than loudly unsupported.

For the trust boundary and design rationale, see [Secret Store Security Model](explanation-secret-store-security-model.md). For the schema and syntax, see [Secret Store Reference](secret_store.md).
