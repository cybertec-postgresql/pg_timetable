## Secret store

The secret store is introduced by migration `00820` and lives entirely in
the `timetable` schema. The schema applies unchanged on a database without
`pgcrypto` installed; the extension is needed only by sessions that
actually resolve a secret.

### Trust and trust boundary

The store is **write-only by design**: there is no plaintext read path;
values are encrypted at rest with `pgp_sym_encrypt` (from the `pgcrypto`
extension) and decrypted only by the `SECURITY DEFINER` function
`timetable.resolve_secret()` when the caller supplies the encryption key.
There is no surrogate `secret_id`; rows are addressed by
`(client_name, secret_name)` only.

The running scheduler process is the trusted execution boundary. The store
raises the bar against other database roles, logical-replica subscribers,
and `pg_dump` archives taken without the encryption key; it does not raise
the bar against a compromised worker host, `ps` / auditd argv inspection,
or any party that holds both `value_enc` and `PGTT_SECRET_KEY`. The
scheduler's connection role can read `value_enc` directly; confidentiality
rests on possession of the encryption key, which the database never stores.

### `pgcrypto` is an optional prerequisite

pg_timetable never installs, requires, or probes for `pgcrypto`. The
migration and the `timetable` schema both apply cleanly on a database
without `pgcrypto` and whose role could not install one. The extension is
the responsibility of the database administrator; the only place it is
looked up is inside `timetable.resolve_secret()` at call time.

### Schema objects

The table and function definitions appear via the `ddl.sql` pymdownx
snippet above. In summary:

- `timetable.secret(client_name TEXT NOT NULL, secret_name TEXT NOT NULL,
  value_enc BYTEA NOT NULL, created_at / updated_at TIMESTAMPTZ NOT NULL
  DEFAULT now(), updated_by TEXT NOT NULL DEFAULT session_user)`.
  `PRIMARY KEY (client_name, secret_name)` — no surrogate `secret_id`.
  `CHECK (secret_name ~ '^[A-Za-z0-9_.-]+$')`. `REVOKE ALL FROM PUBLIC`;
  no default grants to other roles.
- `timetable.secret_touch()` — `BEFORE UPDATE` trigger that refreshes
  `updated_at` / `updated_by` so manual `UPDATE`s cannot leave stale audit
  data. Created with `EXECUTE PROCEDURE` (compatible with every PostgreSQL
  in the supported matrix) and a plain `CREATE TRIGGER`.
- `timetable.resolve_secret(p_name TEXT, p_client TEXT, p_key TEXT)
  RETURNS TEXT` — `LANGUAGE plpgsql`, `SECURITY DEFINER`, `STRICT`,
  `STABLE`, `SET search_path = pg_catalog, timetable`. The pgcrypto lookup
  and the decrypt call live in dynamic SQL. The function locates the
  extension's schema at call time from `pg_catalog.pg_extension` /
  `pg_catalog.pg_namespace`, so `public`, a private schema, and a schema
  set via `ALTER EXTENSION pgcrypto SET SCHEMA ...` all work. Returns
  `NULL` when the `(client_name, secret_name)` pair does not exist
  (without requiring pgcrypto). Raises `feature_not_supported` (SQLSTATE
  `0A000`) when pgcrypto is absent — that single task fails, the scheduler
  keeps running. Raises `Wrong key or corrupt data` on a wrong key.
  `LANGUAGE plpgsql` is required (not `LANGUAGE sql`) so the function
  creates successfully on a database without pgcrypto.
- `timetable.secret_count() RETURNS BIGINT` — `LANGUAGE sql`,
  `SECURITY DEFINER`, `STABLE`. Returns `count(*)` over `timetable.secret`.
  Used by the scheduler startup check when no encryption key is configured;
  does not require `pgcrypto`.

### Reference syntax

`${secret:name}` in `timetable.parameter.value` (jsonb string leaves) or
`timetable.task.database_connection` is replaced at execution time with
the decrypted value of the matching `timetable.secret` row for the
running client. Resolved values are masked from `execution_log.params`,
`timetable.log`, and the pgx tracer's `args` field; see the masking
contract in the spec.

The store is opt-in syntax. Chains created before this feature, whose
`parameter.value` holds a literal password, continue to work unchanged.

### Permission model

- `PUBLIC` has no privileges on the table or the functions.
- The owning role (the scheduler's connection role) can read `value_enc`
  directly — the key, not the grant model, is the confidentiality boundary.
- No new role is created by the schema. Operators who want a separate
  secret-administration role must `GRANT INSERT, UPDATE, DELETE ON
  timetable.secret TO admin_role` themselves; this is an operator step,
  not a schema-managed role.
- The scheduler startup check (`CheckSecretConfig`) calls
  `timetable.secret_count()` exactly once when the encryption key is unset,
  and skips it entirely when the key is set. A failure of the check itself
  is logged, never fatal.
