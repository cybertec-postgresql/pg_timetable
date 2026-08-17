# Database Schema

**pg_timetable** is a database driven application. During the first start the necessary schema is created if absent.

## Main tables and objects

```sql
--8<-- "internal/pgengine/sql/ddl.sql"
```

## Jobs related functions

```sql
--8<-- "internal/pgengine/sql/job_functions.sql"
```

## Сron related functions

```sql
--8<-- "internal/pgengine/sql/cron.sql"
```

## ER-Diagram

![Database Schema](timetable_schema.png)

## Secret store

The secret store is introduced by migration `00798` and lives entirely in
the `timetable` schema. It is the first object created by pg_timetable that
depends on a PostgreSQL extension (`pgcrypto`); the migration installs it
into `timetable` so the `SECURITY DEFINER` decryption function can pin a
trusted `search_path`.

**Schema:**

- `timetable.secret` — `(client_name TEXT NOT NULL, secret_name TEXT NOT NULL,
  value_enc BYTEA NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_by TEXT NOT NULL
  DEFAULT session_user)`. PK `(client_name, secret_name)`. CHECK
  `secret_name ~ '^[A-Za-z0-9_.-]+$'`. `REVOKE ALL` from PUBLIC; no other
  role receives default grants.
- `timetable.secret_touch()` — `BEFORE UPDATE` trigger that refreshes
  `updated_at`/`updated_by` so manual UPDATEs cannot leave stale audit data.
- `timetable.resolve_secret(name TEXT, client TEXT, key TEXT) RETURNS TEXT`
  — `SECURITY DEFINER`, `STRICT`, `STABLE`, `SET search_path =
  pg_catalog, timetable`. Decrypts via `<pgcrypto_schema>.pgp_sym_decrypt`.
  Returns `NULL` when the `(client, name)` pair does not exist; raises on a
  wrong key.
- `timetable.secret_count() RETURNS BIGINT` — non-sensitive row count used

DB readers, backups, dumps, and audit-log spill, not against a compromised
worker host. Decrypted values are not redacted from `args` bindings on
`resolve_secret(...)` calls — those bindings are not persisted.

*ER-Diagram showing the database structure*