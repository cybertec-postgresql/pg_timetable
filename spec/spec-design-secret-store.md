---
title: Postgres-Native Secret Store (`timetable.secret`) for pg_timetable
version: 2.0
date_created: 2026-08-17
last_updated: 2026-08-17
owner: pg_timetable maintainers
tags: [design, schema, security, app]
---

# Introduction

pg_timetable stores task-time credentials (SMTP passwords, remote-database
connection strings, PROGRAM tokens) as plaintext in
`timetable.parameter.value` and `timetable.task.database_connection`, and
persists parameter values verbatim to `timetable.execution_log.params`, to
scheduler debug logs, and — at debug level — to the `timetable.log` table via
the pgx query tracer. This specification defines a Postgres-native,
GitHub-Actions-shaped secret store (`timetable.secret`), a `${secret:name}`
reference syntax resolved by the scheduler in-process immediately before use,
and the mandatory masking changes that make the store meaningful rather than
theater.

This document is self-contained. It supersedes and absorbs the content of the
prior exploration and design-brief documents; no external document is required
to implement, review, or verify this specification.

## 1. Purpose & Scope

**Purpose.** Define the exact schema, extension handling, ownership/grant
model, Go interfaces, call-site changes, escaping rules, failure semantics,
and migration/documentation obligations required to ship a named, write-only,
masked-on-output secret store for pg_timetable, modeled on GitHub Actions
repository secrets.

### 1.1 Product model

The reference point is **GitHub Actions repository secrets**, not a
general-purpose vault:

| GitHub Actions secrets | pg_timetable equivalent |
|---|---|
| `Settings → Secrets and variables` | `timetable.secret` catalog table |
| `${{ secrets.SMTP_PASSWORD }}` in workflow YAML | `"password": "${secret:smtp_main}"` in task parameters |
| Write-only (value not readable back) | No plaintext `SELECT` path; decryption only via `timetable.resolve_secret()` with the key |
| Masked in run logs (`***`) | Reference form persisted to `execution_log.params`; resolved values kept out of all logs |
| No rotation/versioning/leasing | Same — deliberately absent |
| Scoped to repo or org | Scoped to exactly one `client_name` |

**Scope — in.**

- `timetable.secret` table, its constraint, comments, trigger, and ownership
  model, added to **both** the fresh-install DDL and a new migration.
- `pgcrypto` extension acquisition with deterministic schema resolution.
- `timetable.resolve_secret()` and `timetable.secret_count()`
  `SECURITY DEFINER` functions.
- `${secret:name}` reference syntax in `timetable.parameter.value` (jsonb
  string leaves) and `timetable.task.database_connection` (libpq conninfo).
- Go resolution helpers in `internal/pgengine/secrets.go` and their wiring
  into builtin `SendMail`, remote/autonomous/local SQL execution, and PROGRAM
  argv construction.
- Masking of `timetable.execution_log.params`, the scheduler builtin debug
  log, and the pgx tracer's `args` field (which otherwise persists resolved
  values into the `timetable.log` table).
- New config field `SecretEncryptionKey` (`--secret-key` / `PGTT_SECRET_KEY`).
- Migration of `samples/Mail.sql` and `samples/RemoteDB.sql`, the test
  harness changes those samples require, and documentation updates.

**Scope — out** (deliberate product boundaries, not deferred work):

- Secret rotation, versioning, leasing, or dynamic credentials.
- External KMS / HashiCorp Vault / cloud-secret-manager integration.
- Multi-backend plugin architecture.
- Usage audit trail beyond `updated_by`/`updated_at`.
- Replacing `.pgpass` / `.pg_service.conf` / env / `ConnStr` for the
  scheduler's own Postgres login.
- Encrypting or hiding SQL command text itself.
- References inside `timetable.task.command`.
- YAML authoring UX for secret references (`samples/yaml/*.yaml` unchanged).
- Creating, owning, or managing Postgres roles (pg_timetable creates no roles
  today and will not start).
- Any guarantee of confidentiality against a compromised worker host,
  `ps`/auditd argv inspection, or a party holding both `value_enc` and the
  encryption key.

**Intended audience.** pg_timetable maintainers and contributors implementing
this feature; AI coding agents generating the implementation; reviewers
verifying the implementation against this contract.

## 2. Definitions

- **Chain**: an ordered sequence of tasks, identified by
  `timetable.chain.chain_id`.
- **Task**: a unit of work within a chain (`timetable.task`), of kind `SQL`,
  `PROGRAM`, or `BUILTIN`.
- **Parameter**: a jsonb value (`timetable.parameter.value`) supplying
  arguments to a task invocation. Retrieved as raw jsonb text by
  `PgEngine.GetChainParamValues` (`internal/pgengine/access.go`).
- **Builtin task**: a task whose `command` names a Go function registered in
  `scheduler.BuiltinTasks` (`SendMail`, `Log`, `NoOp`, `Sleep`, `Shutdown`,
  `Download`, the `Copy*` family).
- **`client_name`**: the mandatory single identity of a scheduler process,
  supplied by `-c/--clientname` (`CmdOptions.ClientName`). `internal/config`'s
  `NewConfig` returns an error when it is unset, so every running scheduler
  has exactly one.
- **Secret**: a named, encrypted value stored in `timetable.secret`,
  referenced via `${secret:name}` and never returned in plaintext by any
  `SELECT`-accessible path.
- **Reference (unresolved form)**: the literal string `${secret:name}` as
  stored in the database.
- **Resolved form**: the plaintext value substituted for a reference,
  produced only in scheduler process memory, immediately before use.
- **Masking**: ensuring the resolved form is never written to
  `timetable.execution_log.params`, never written to `timetable.log`, and
  never emitted to stdout/file logs at any level.
- **`SECURITY DEFINER`**: a Postgres function execution mode that runs with
  the privileges of the function's **owner** rather than its caller.
- **pgcrypto**: the Postgres contrib extension providing `pgp_sym_encrypt` /
  `pgp_sym_decrypt`. Since PostgreSQL 13 it is a **trusted** extension,
  installable by a non-superuser holding `CREATE` on the database.
- **pgx tracer**: `tracelog.TraceLog` installed on the connection pool in
  `internal/pgengine/bootstrap.go`, which logs each query's SQL **and bound
  arguments** when the log level is debug.
- **Secret scoping (divergent from chain)**: `timetable.chain.client_name` is
  nullable and `NULL` means "any client may run this" — a scheduling
  convenience. `timetable.secret.client_name` is `NOT NULL` with no global
  equivalent — a security boundary.

## 3. Requirements, Constraints & Guidelines

### Schema requirements

- **REQ-001**: The system MUST provide a table `timetable.secret` with columns
  `client_name TEXT NOT NULL`, `secret_name TEXT NOT NULL`,
  `value_enc BYTEA NOT NULL`, `created_at TIMESTAMPTZ NOT NULL DEFAULT now()`,
  `updated_at TIMESTAMPTZ NOT NULL DEFAULT now()`,
  `updated_by TEXT NOT NULL DEFAULT session_user`, with
  `PRIMARY KEY (client_name, secret_name)`. There MUST be no surrogate
  `secret_id` column: secrets are addressed only by
  `(client_name, secret_name)`.
- **REQ-002**: `secret_name` MUST carry `CHECK (secret_name ~ '^[A-Za-z0-9_.-]+$')`
  under the constraint name `secret_name_format`.
- **REQ-003**: `client_name` MUST be `NOT NULL` and MUST NOT support a
  "visible to all clients" mode. A secret needed by multiple clients MUST be
  inserted once per client.
- **REQ-004**: `value_enc` MUST store ciphertext produced by
  `pgp_sym_encrypt()`. No column, view, or function may return plaintext
  except `timetable.resolve_secret()` when supplied the correct key.
- **REQ-005**: The table MUST carry `COMMENT ON TABLE` and
  `COMMENT ON COLUMN` text stating the write-only model, the absence of
  rotation/versioning, the mandatory client scoping, and that resolution
  occurs only via `resolve_secret`.
- **REQ-006**: `timetable.secret` MUST have a `BEFORE UPDATE ... FOR EACH ROW`
  trigger that sets `NEW.updated_at := now()` and
  `NEW.updated_by := session_user`. Without it the audit columns silently
  retain insert-time values after every `UPDATE`. The trigger MUST be created
  with `EXECUTE PROCEDURE` (accepted by every PostgreSQL version in the
  project's supported matrix, unlike PG11+ `EXECUTE FUNCTION`) and with plain
  `CREATE TRIGGER` (not PG14+ `CREATE OR REPLACE TRIGGER`).

### Extension requirements

- **REQ-007**: The implementation MUST NOT assume `pgcrypto`'s schema.
  `CREATE EXTENSION IF NOT EXISTS pgcrypto` installs into the first writable
  schema of the installing session's `search_path` (normally `public`), which
  is not reachable from a function pinned to
  `SET search_path = pg_catalog, timetable`. The unqualified
  `pgp_sym_decrypt` call would then fail at runtime.
- **REQ-008**: Schema creation MUST therefore (a) install `pgcrypto` into
  `timetable` when the extension is absent, (b) detect the schema it actually
  occupies via `pg_catalog.pg_extension`/`pg_catalog.pg_namespace`, and
  (c) create `timetable.resolve_secret` with `pgp_sym_decrypt`
  **schema-qualified to that schema**, by generating the `CREATE FUNCTION`
  through `EXECUTE format(...)` with a `%I` placeholder. Raise an exception
  when the extension is still absent after step (a). See §4.1 for the exact
  SQL.
- **REQ-053**: Qualifying inside the body is REQUIRED; pinning an
  unqualified body and repairing it afterwards with
  `ALTER FUNCTION ... SET search_path` does NOT work. PostgreSQL validates a
  `LANGUAGE sql` body at creation time against the pinned `search_path`, so
  `CREATE FUNCTION` itself fails with
  `function pgp_sym_decrypt(bytea, text) does not exist` on any database
  where `pgcrypto` is not in `timetable`. Verified on PostgreSQL 16.1 against
  a database with `pgcrypto` pre-installed in `public`.
- **REQ-052**: Because installing `pgcrypto` into `timetable` puts
  `pgp_sym_encrypt` outside the default `search_path`, every **write-side**
  call — in samples, documentation, and tests — MUST either schema-qualify it
  as `timetable.pgp_sym_encrypt(...)` or run with `timetable` on the
  session `search_path`. Verified against PostgreSQL 16: with the extension
  in `timetable`, an unqualified `pgp_sym_encrypt('x', 'k')` from a default
  session fails with `function pgp_sym_encrypt(unknown, unknown) does not
  exist`, while both the qualified form and `SET search_path = public,
  timetable` succeed. This affects REQ-047 and REQ-048 directly, since
  `samples/*.sql` execute in a plain session via `ExecuteCustomScripts`.
- **CON-001**: No other object in `internal/pgengine/sql/` issues
  `CREATE EXTENSION` today; this feature introduces the project's first
  extension dependency and MUST fail the migration loudly (rather than
  degrade) if `pgcrypto` cannot be installed.

### Ownership and access-control requirements

- **REQ-009**: The implementation MUST NOT reference role names that
  pg_timetable does not create. `internal/pgengine/sql/` contains no
  `CREATE ROLE`, `GRANT`, or `REVOKE` statement, and the migrator wraps each
  migration in a single transaction, so a `GRANT ... TO
  <nonexistent_role>` aborts the whole migration and blocks startup
  permanently. Therefore no new role is invented, and every object created by
  this feature is owned by the role that runs schema creation — i.e. the
  scheduler's own connection role.
- **REQ-010**: `PUBLIC` MUST have zero privileges on `timetable.secret`
  (`REVOKE ALL ON timetable.secret FROM PUBLIC`) and zero `EXECUTE` on both
  new functions (`REVOKE ALL ON FUNCTION ... FROM PUBLIC`, required because
  new functions grant `EXECUTE` to `PUBLIC` by default).
- **REQ-011**: `timetable.resolve_secret(p_name TEXT, p_client TEXT, p_key TEXT)
  RETURNS TEXT` MUST be `LANGUAGE sql`, `SECURITY DEFINER`, `STABLE`,
  `STRICT`, and pinned via `SET search_path` per REQ-008.
- **REQ-012**: `resolve_secret` MUST match `client_name = p_client AND
  secret_name = p_name` exactly — no `OR client_name IS NULL` fallback, no
  `ORDER BY`/`LIMIT` tie-break. The composite primary key guarantees at most
  one matching row.
- **REQ-013**: `timetable.secret_count() RETURNS BIGINT` MUST exist as
  `LANGUAGE sql`, `SECURITY DEFINER`, `STABLE`, returning `count(*)` over
  `timetable.secret`. It exists because the startup check in REQ-020 cannot
  read the table directly under the no-extra-grants model of REQ-010.
- **SEC-001**: Documentation MUST state the true security property rather than
  an overclaim: because the table and both functions are owned by the
  scheduler's role, that role can read `value_enc` directly. Confidentiality
  rests on **possession of the encryption key**, which the database never
  stores, plus the absence of any privilege for other roles. The
  `SECURITY DEFINER` + `REVOKE FROM PUBLIC` model exists to keep every
  *other* database role — reporting/Grafana roles, `run_as` roles used with
  `SET ROLE`, ad hoc DBA sessions — away from both the ciphertext and the
  decryption path.
- **REQ-014**: No additional `SELECT` grant on `timetable.secret` may be
  issued to any role. Operators who want a separate secret-administration
  role MUST grant `INSERT, UPDATE, DELETE` manually; this is an operator step
  documented per REQ-041, not a schema-managed role.

### Key-management requirements

- **REQ-015**: The system MUST add `SecretEncryptionKey` to
  `config.CmdOptions` in `internal/config/cmdparser.go` with tags
  `long:"secret-key"`, `mapstructure:"secret-key"`,
  `env:"PGTT_SECRET_KEY"`.
- **REQ-016**: The `mapstructure:"secret-key"` tag is MANDATORY, not
  stylistic. `NewConfig` binds flags into viper under their long names and
  then calls `v.Unmarshal(conf)`; viper matches the `secret-key` key to the
  `SecretEncryptionKey` field only through an explicit `mapstructure` tag.
  Verified experimentally inside `internal/config`: with the tag absent, a
  viper key `secret-key` unmarshals to the empty string while
  `no-program-tasks` (tagged) and `connstr` (name-identical to its field)
  populate correctly. `ClientName`/`ConnStr` are therefore the wrong
  precedent; the correct precedent is `NoProgramTasks`
  (`mapstructure:"no-program-tasks"`).
- **REQ-017**: The encryption key MUST NOT be stored in `timetable.secret` or
  any other database table.
- **REQ-018**: The key MUST NOT be passed to any traced database call without
  the redaction of REQ-030 in effect; `p_key` is a bound query argument and
  would otherwise be logged verbatim by the pgx tracer at debug level.
- **CON-002**: If `SecretEncryptionKey` is unset and `timetable.secret` is
  empty, the feature MUST be inert: no startup error, no behavior change, and
  zero added cost per task execution (the substring pre-check of REQ-026
  short-circuits before any parsing or database round-trip). Exactly one
  additional query per process start is permitted, and only when the key is
  unset (REQ-020).
- **REQ-019**: When `SecretEncryptionKey` is non-empty the startup check MUST
  be skipped entirely — no `secret_count()` call.
- **REQ-020**: When `SecretEncryptionKey` is empty, the scheduler MUST call
  `timetable.secret_count()` once at startup and log an error when the count
  is greater than zero. The check MUST live in a new
  `func (pge *PgEngine) CheckSecretConfig(ctx context.Context) error` in
  `internal/pgengine/secrets.go`, invoked from `run()` in `main.go` after the
  migration/upgrade block (so the schema is known current) and before
  `scheduler.New`. A failure of the check itself MUST be logged, not fatal.

### Reference syntax requirements

- **REQ-021**: The syntax MUST be exactly `${secret:name}` where `name`
  matches `^[A-Za-z0-9_.-]+$`. No nested braces, no default-value fallback.
- **REQ-022**: References MUST be resolvable only in (a) string leaves of
  `timetable.parameter.value` and (b) `timetable.task.database_connection`.
- **REQ-023**: References MUST NOT be resolved inside
  `timetable.task.command`; command text is logged verbatim by design.
- **REQ-024**: A resolved value MUST NOT be re-scanned for further
  `${secret:...}` patterns.
- **REQ-025**: Matching MUST use the Go regexp
  `\$\{secret:([A-Za-z0-9_.-]+)\}`.
- **REQ-026**: Resolution MUST be skipped entirely — no JSON parsing, no
  regexp evaluation, no database call, and byte-identical output — for any
  input that does not contain the literal substring `${secret:`. This is a
  mandatory correctness and inertness guarantee: it preserves the exact
  existing behavior (including existing malformed-JSON error paths) for every
  parameter that uses no secrets.

### Escaping requirements

- **REQ-027**: jsonb-carried parameters MUST NOT be resolved by flat string
  substitution on the raw jsonb text. A secret value containing `"`, `\`, or
  a newline would produce malformed JSON and break the downstream
  `json.Unmarshal` in `taskSendMail`, `ExecuteSQLCommand`, and
  `ExecuteProgramCommand`. Resolution MUST instead decode the parameter,
  substitute within string leaves only, and re-encode with `encoding/json`,
  which performs the escaping.
- **REQ-028**: `database_connection` values MUST be substituted with libpq
  conninfo quoting applied to each resolved value:
  - If the value is empty or contains a space, tab, newline, `'`, or `\`, it
    MUST be wrapped in single quotes with every `\` and `'` backslash-escaped.
  - If the reference in the template is already immediately delimited by
    single quotes (for example `password='${secret:pw}'`), the wrapping MUST
    be omitted and only `\` and `'` escaped, so the existing delimiters are
    not doubled.
- **REQ-029**: Resolved values MUST NOT be re-encoded in any way not
  specified above; in particular no URL-encoding, trimming, or case folding.

### Masking requirements (cross-cutting, mandatory for v1)

- **REQ-030**: The pgx tracer leaks resolved values into the **database**.
  `internal/pgengine/bootstrap.go` installs `tracelog.TraceLog` with
  `LogLevelDebug` when `--log-level=debug`; `tracelog` logs
  `{"sql": ..., "args": logQueryArgs(args)}` for every query, and
  `logQueryArgs` only truncates arguments over 64 bytes — it does not redact.
  Those entries flow through `log.PgxLogger.Log` → logrus → `LogHook.send` →
  `CopyFrom` into `timetable.log(message, message_data)`, and `LogHook.Levels`
  returns `logrus.AllLevels` when `--log-database-level=debug`. The
  implementation MUST therefore add a context-scoped redaction marker:
  - `internal/log` gains `WithoutQueryArgs(ctx context.Context) context.Context`
    and `PgxLogger.Log` MUST delete the `args` key from `data` when the
    marker is present. pgx passes the caller's context through
    `TraceQueryStart`/`TraceQueryEnd` to `Logger.Log`, so the marker reaches
    the logger unmodified. The `sql` key is retained (command text is
    logged verbatim by design, REQ-023).
  - Every database call that carries a resolved secret value or the
    encryption key as a bound argument MUST pass a marked context: the
    `resolve_secret` call inside `ResolveSecrets` (carries `p_key`) and
    `executor.Exec(ctx, task.Command, params...)` in `ExecuteSQLCommand`
    whenever resolution substituted at least one secret.
- **REQ-031**: `LogTaskExecution` (`internal/pgengine/access.go`) MUST never
  receive a resolved string in its `params` argument. There are **three**
  call sites, all of which MUST pass the pre-resolution reference form:
  `internal/pgengine/transaction.go` (`ExecuteSQLCommand`),
  `internal/scheduler/tasks.go` (`executeBuiltinTask`), and
  `internal/scheduler/shell.go` (`ExecuteProgramCommand`). `LogTaskExecution`
  itself needs no signature change.
- **REQ-032**: The `Debugf` call in `executeBuiltinTask`
  (`l.WithField("name", name).Debugf("Executing builtin task with parameters %+q", paramValues)`)
  MUST NOT print full parameter values. It MUST be changed to log the
  parameter count only (values may contain secrets after a future change, and
  reference-form values carry no useful debug information beyond their
  presence).
- **REQ-033**: `executeBuiltinTask` MUST NOT resolve secrets and MUST NOT
  rebind its loop variable `val`, because the same `val` is passed to
  `LogTaskExecution` on the following line. Builtin resolution happens inside
  the individual builtin handler that consumes a secret — in v1 exactly
  `taskSendMail`.
- **REQ-034**: `taskSendMail`'s signature MUST change from
  `func taskSendMail(ctx context.Context, _ *Scheduler, paramValues string)`
  to bind the scheduler receiver (`sch *Scheduler`) so that
  `sch.pgengine.ResolveSecretsJSON` is reachable. The `BuiltinTasks` map type
  is unchanged.
- **CON-003**: `internal/tasks/mail.go` MUST NOT be modified. It continues to
  operate on an already-resolved `tasks.EmailConn`.
- **REQ-035**: Resolved values MUST NOT be added to any OpenTelemetry span
  attribute or metric label. `internal/otel` attaches only `client.name`,
  `task.name`, `task.kind`, and `task.return_code` today and MUST stay that
  way.

### Resolution-point requirements

- **REQ-036**: `taskSendMail` MUST call `ResolveSecretsJSON` on its
  `paramValues` argument before `json.Unmarshal` into `tasks.EmailConn`.
- **REQ-037**: `ExecuteSQLCommand` MUST resolve each loop value with
  `ResolveSecretsJSON` into a separate variable, unmarshal the **resolved**
  text into `params`, pass a REQ-030-marked context to `executor.Exec` when
  any secret was substituted, and pass the **unresolved** `val` to
  `LogTaskExecution`.
- **REQ-038**: `ExecRemoteSQLTask` MUST resolve `task.ConnectString` with
  `ResolveSecretsConnString` **eagerly**, before constructing the
  `func() (PgxConnIface, error)` closure it hands to `ExecStandaloneTask`.
  Resolving inside the closure would defer the error until after `SetRole`
  and `SetCurrentTaskContext` have already run. The resolved string MUST be
  captured in a local variable; `task.ConnectString` MUST NOT be overwritten.
- **REQ-039**: `ExecuteProgramCommand` (`internal/scheduler/shell.go`) MUST
  resolve each loop value with `ResolveSecretsJSON` into a separate variable,
  unmarshal the resolved text into `params`, and pass the unresolved `val` to
  `LogTaskExecution`.
- **REQ-040**: Resolution MUST occur at the narrowest scope described above
  and never earlier in the call chain; in particular never in
  `GetChainParamValues`, never in `internal/pgengine/types.go`, and never in
  `executeTask`.

### Failure-semantics requirements

- **REQ-041**: The three failure classes MUST be distinguished, because the
  underlying SQL behaves differently in each:
  1. **Missing secret** — a `LANGUAGE sql` scalar function whose final query
     matches no row returns **NULL**, not zero rows (PostgreSQL: "If the last
     query happens to return no rows at all, the null value will be
     returned"). `SELECT timetable.resolve_secret(...)` therefore yields
     exactly one row containing NULL, so the implementation MUST scan into a
     nullable target (`*string` or `pgtype.Text`) and treat NULL as
     not-found. It MUST NOT rely on `pgx.ErrNoRows`, which never occurs on
     this path. Error text MUST name the secret and the client scope, e.g.
     `secret "smtp_main" not found for client "worker-1"`.
  2. **Key unset** — when the input contains a reference and
     `SecretEncryptionKey` is empty, `ResolveSecrets*` MUST fail before
     issuing any query, with an error naming the missing configuration. This
     check is required because `pgp_sym_encrypt(x, '')` is legal, so an empty
     key otherwise produces a confusing corrupt-data error from Postgres.
  3. **Wrong key** — `pgp_sym_decrypt` raises `Wrong key or corrupt data`.
     The error MUST be wrapped with the secret name and MUST NOT be
     conflated with class 1.
- **REQ-042**: Silent empty-string substitution is PROHIBITED in every class.
- **REQ-043**: Failures MUST propagate as Go `error` values from
  `ResolveSecretsJSON`/`ResolveSecretsConnString` and from every caller,
  following the existing conventions (`errors.Join`, early `return`).
- **REQ-044**: A secret existing only under a different `client_name` MUST be
  indistinguishable from a nonexistent secret, so that secret existence does
  not leak across client boundaries.

### Migration / documentation requirements

- **REQ-045**: The schema objects MUST be added to **both**
  `internal/pgengine/sql/ddl.sql` and
  `internal/pgengine/sql/migrations/00798.sql`, with identical object
  definitions. A migration alone is insufficient: `ExecuteSchemaScripts`
  runs `{init, cron, ddl, json_schema, job_functions}` only when the
  `timetable` schema is absent, and `sql/init.sql` seeds
  `timetable.migration` with every version through the current release — so
  on a fresh database `NeedUpgrade` is false and the new migration never
  executes. The established pattern is a dual write, as done for `00792`
  (`task.live`, also in `ddl.sql`), `00797` (execution_log indexes, also in
  `ddl.sql`), and `00733` (`execution_log.params`, also in `ddl.sql`).
- **REQ-046**: The registration MUST touch **three** files, as the in-code
  comment in `internal/pgengine/migration.go` states ("adding new migration
  here, update `timetable.migration` in `sql/init.sql` and `dbapi` variable
  in `main.go`!"):
  1. `internal/pgengine/migration.go` — appended `&migrator.Migration{...}`
     entry named `00798 Add timetable.secret store`.
  2. `internal/pgengine/sql/init.sql` — `(18, '00798 Add timetable.secret store')`
     appended to the seed `INSERT`.
  3. `main.go` — `dbapi = "00798"`.
  The number `00798` is the next available after the current highest
  migration `00797`; it MUST be reconfirmed against `migration.go` at
  implementation time in case another migration lands first, and all four
  occurrences (file name, `migration.go` entry, `init.sql` row, `dbapi`) MUST
  agree.
- **REQ-047**: `samples/Mail.sql` MUST insert a secret row via
  `timetable.pgp_sym_encrypt` (schema-qualified per REQ-052, because samples
  run in a plain session) scoped to an explicit `client_name` placeholder, change
  the `"password"` parameter field to `"${secret:smtp_main}"`, and retain a
  `-- Legacy (deprecated):` comment showing the prior inline literal. The
  legacy inline-literal form MUST continue to work unchanged for chains
  created before this feature ships; `${secret:...}` is opt-in syntax, not a
  format change.
- **REQ-048**: `samples/RemoteDB.sql` MUST replace `password=somestrong`
  with `password=${secret:remotedb_demo}`, insert the corresponding secret
  row using `timetable.pgp_sym_encrypt` under the same client-name
  convention, and note that the demo is same-cluster while the pattern
  applies to genuine cross-host connections.
- **REQ-049**: The sample changes break two currently-passing tests and MUST
  be accompanied by harness changes. `internal/pgengine/pgengine_test.go`'s
  `TestSamplesScripts` executes every file in `samples/` and then runs the
  scheduler; `internal/scheduler/scheduler_test.go`'s `TestRun` executes
  `samples/RemoteDB.sql` and runs the chain. Both use
  `testutils.SetupPostgresContainer`, which builds
  `config.NewCmdOptions("--clientname=testcontainers_unit_test", "--connstr="+connStr)`
  — no encryption key, no secret rows, and a `client_name` that does not
  match a sample placeholder. Therefore:
  - `internal/testutils/testcontainers.go` MUST set a fixed test
    `SecretEncryptionKey` on the constructed `CmdOptions`.
  - The samples MUST derive `client_name` from
    `current_setting('pg_timetable.current_client_name', true)` where
    available, or use a documented placeholder plus a companion insert that
    the harness performs; the chosen mechanism MUST make
    `samples/*.sql` self-contained under `TestSamplesScripts`, which runs
    them with no manual setup.
  - The samples MUST use the same key literal as the harness so decryption
    succeeds in tests.
- **REQ-050**: `docs/samples.md` and `docs/yaml-usage-guide.md` MUST gain a
  "Secrets" subsection documenting `${secret:name}`, the write-only model,
  the manual grant step of REQ-014, the PROGRAM argv caveat of SEC-003, the
  debug-level caveat of SEC-004, and the trust boundary of SEC-002.
- **REQ-051**: `docs/database_schema.md` embeds `ddl.sql` verbatim through a
  pymdownx snippet, so the table and functions are documented automatically
  by REQ-045; the page MUST additionally gain prose covering the write-only
  model and `resolve_secret` usage.
- **CON-004**: No changes to `samples/yaml/*.yaml` in v1.

### Security requirements

- **SEC-002**: Documentation introduced by this feature MUST state
  explicitly: the worker process is the trusted execution boundary; this
  feature defends against other database roles, backups, `pg_dump` output,
  and logical replicas; it does **not** defend against a compromised worker
  host, `ps`/argv inspection, or any party holding both `value_enc` and the
  key.
- **SEC-003**: Because v1 resolves references in PROGRAM parameters, resolved
  values become process argv and are visible to `ps` and auditd on the worker
  host. This MUST be documented as an accepted limitation of the PROGRAM
  path.
- **SEC-004**: Running with `--log-level=debug` and
  `--log-database-level=debug` MUST be documented as the configuration in
  which query arguments are logged; the REQ-030 redaction is what keeps
  resolved secrets and the encryption key out of those entries, and any new
  traced call carrying secret material MUST adopt the same marker.
- **SEC-005**: `pgcrypto`'s `pgp_sym_encrypt`/`pgp_sym_decrypt` MUST be the
  only cryptographic primitive; no custom cipher is permitted.
- **SEC-006**: Every function introduced MUST have an explicit
  `REVOKE ALL ... FROM PUBLIC`.

### Constraints

- **CON-005**: This feature MUST NOT alter the scheduler's own Postgres
  connection or authentication mechanism.
- **CON-006**: This feature MUST NOT introduce versioning, rotation, leasing,
  or external KMS integration.
- **CON-007**: The composite primary key means updating a secret overwrites it
  in place — no history, no rollback. The same `secret_name` under different
  `client_name` values are distinct secrets, not versions.
- **CON-008**: `GetRemoteDBConnection` calls `pgx.Connect`, which creates a
  connection **without** the pool's tracer, so remote conninfo is not traced.
  pgx also redacts passwords in its own connection errors:
  `ParseConfigError.Error` applies `redactPW` to the connection string and
  `ConnectError.Error` prints only user and database. No additional work is
  required on that path, and none may be added.

### Guidelines

- **GUD-001**: PROGRAM secret delivery via environment variable or a
  short-lived file/fd is preferable to argv interpolation. v1 implements argv
  substitution because the alternative — passing the literal `${secret:x}`
  through to the child process — is silently wrong behavior rather than a
  loud limitation. A future revision SHOULD add env/fd delivery; SEC-003
  documents the current exposure.
- **GUD-002**: New Go code MUST follow the existing package layout:
  engine-level database access in `internal/pgengine`, scheduler
  orchestration in `internal/scheduler`, logging concerns in `internal/log`,
  no new top-level package.
- **GUD-003**: Prefer `.pgpass` / `.pg_service.conf` on the worker host over
  `${secret:...}` for remote Postgres passwords. The secret store exists for
  cases where host-local credential files are not available, and for
  non-Postgres credentials such as SMTP.

### Patterns to follow

- **PAT-001**: Reuse the `client_name` column name and type from
  `timetable.chain.client_name` for consistency, but deliberately not its
  nullability (REQ-003).
- **PAT-002**: Reuse the dual-write migration convention exactly as done for
  `00792`, `00797`, and `00733`: `migrations/00NNN.sql` + `ddl.sql` +
  `migration.go` + `init.sql` + `main.go` `dbapi`.
- **PAT-003**: Reuse the `NoProgramTasks` config-field pattern
  (`long` + `mapstructure` + `env` tags) for `SecretEncryptionKey`. Do not
  copy `ClientName`/`ConnStr`, which work only incidentally (REQ-016).

## 4. Interfaces & Data Contracts

### 4.1 SQL schema

This block is appended verbatim to `internal/pgengine/sql/ddl.sql` and
duplicated in `internal/pgengine/sql/migrations/00798.sql`.

```sql
-- Ensure pgcrypto exists (REQ-007/REQ-008).
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_extension WHERE extname = 'pgcrypto') THEN
        EXECUTE 'CREATE EXTENSION pgcrypto SCHEMA timetable';
    END IF;
END;
$$;

CREATE TABLE timetable.secret (
    client_name  TEXT        NOT NULL,   -- REQUIRED security boundary: no NULL/global secrets
    secret_name  TEXT        NOT NULL,
    value_enc    BYTEA       NOT NULL,   -- pgp_sym_encrypt() ciphertext, never plaintext
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by   TEXT        NOT NULL DEFAULT session_user,
    PRIMARY KEY (client_name, secret_name)
);

ALTER TABLE timetable.secret ADD CONSTRAINT secret_name_format
    CHECK (secret_name ~ '^[A-Za-z0-9_.-]+$');

COMMENT ON TABLE timetable.secret IS
    'Write-only, named secret values referenced from task parameters and connection strings as ${secret:name}, scoped to exactly one client_name. Modeled on GitHub Actions repository secrets: no plaintext read path, no rotation, no versioning, no cross-client sharing.';
COMMENT ON COLUMN timetable.secret.client_name IS
    'Owning client. Mandatory security boundary: resolvable only by the scheduler process running with this exact client_name (-c/--clientname). Unlike timetable.chain.client_name, NULL/global is not permitted.';
COMMENT ON COLUMN timetable.secret.secret_name IS
    'Reference key used in ${secret:name} syntax, unique within client_name. Case-sensitive, no whitespace (enforced by CHECK secret_name_format).';
COMMENT ON COLUMN timetable.secret.value_enc IS
    'pgcrypto-encrypted value. Decrypted only by timetable.resolve_secret() when supplied the key configured as PGTT_SECRET_KEY, which the database never stores.';
COMMENT ON COLUMN timetable.secret.updated_by IS
    'session_user that last inserted or updated this row, maintained by the secret_touch trigger.';

REVOKE ALL ON timetable.secret FROM PUBLIC;
-- No grants to other roles. Objects are owned by the schema-creating (scheduler)
-- role; a separate administrative role is an operator-managed GRANT.

CREATE OR REPLACE FUNCTION timetable.secret_touch() RETURNS trigger AS
$CODE$
BEGIN
    NEW.updated_at := now();
    NEW.updated_by := session_user;
    RETURN NEW;
END;
$CODE$
LANGUAGE plpgsql;

COMMENT ON FUNCTION timetable.secret_touch() IS
    'Keeps timetable.secret.updated_at/updated_by truthful on UPDATE';

CREATE TRIGGER secret_touch
    BEFORE UPDATE ON timetable.secret
    FOR EACH ROW EXECUTE PROCEDURE timetable.secret_touch();

-- Create resolve_secret with the decrypt call schema-qualified to whichever
-- schema pgcrypto actually occupies: 'timetable' on fresh installs, but
-- possibly 'public' or another schema where pgcrypto pre-existed (REQ-008).
--
-- The qualification must be baked into the body at creation time. A plain
-- CREATE FUNCTION with an unqualified pgp_sym_decrypt plus a later
-- ALTER FUNCTION ... SET search_path does NOT work: PostgreSQL validates a
-- LANGUAGE sql body when the function is created, using the pinned
-- search_path, so creation itself fails with
-- `function pgp_sym_decrypt(bytea, text) does not exist` on any database
-- where pgcrypto is not in `timetable`. Verified on PostgreSQL 16.1.
DO $OUTER$
DECLARE
    v_ext_schema TEXT;
BEGIN
    SELECT n.nspname INTO v_ext_schema
    FROM pg_catalog.pg_extension e
    JOIN pg_catalog.pg_namespace n ON n.oid = e.extnamespace
    WHERE e.extname = 'pgcrypto';

    IF v_ext_schema IS NULL THEN
        RAISE EXCEPTION 'pgcrypto extension is required by timetable.secret but is not installed';
    END IF;

    EXECUTE format($SQL$
        CREATE OR REPLACE FUNCTION timetable.resolve_secret(p_name TEXT, p_client TEXT, p_key TEXT)
        RETURNS TEXT
        LANGUAGE sql
        SECURITY DEFINER
        STABLE
        STRICT
        SET search_path = pg_catalog, timetable
        AS $BODY$
            SELECT %I.pgp_sym_decrypt(value_enc, p_key)
            FROM timetable.secret
            WHERE client_name = p_client
              AND secret_name = p_name;
        $BODY$;
    $SQL$, v_ext_schema);
END;
$OUTER$;

COMMENT ON FUNCTION timetable.resolve_secret(TEXT, TEXT, TEXT) IS
    'Returns the decrypted value of one secret, or NULL when the (client_name, secret_name) pair does not exist. Raises when the key is wrong.';

REVOKE ALL ON FUNCTION timetable.resolve_secret(TEXT, TEXT, TEXT) FROM PUBLIC;

CREATE OR REPLACE FUNCTION timetable.secret_count()
RETURNS BIGINT
LANGUAGE sql
SECURITY DEFINER
STABLE
SET search_path = pg_catalog, timetable
AS $$
    SELECT count(*) FROM timetable.secret;
$$;

COMMENT ON FUNCTION timetable.secret_count() IS
    'Number of stored secrets, used by the scheduler startup check when no encryption key is configured. Exposes no secret material.';

REVOKE ALL ON FUNCTION timetable.secret_count() FROM PUBLIC;

```

### 4.2 Go interfaces — `internal/pgengine/secrets.go`

```go
// secretRefPattern matches ${secret:name}; the character class mirrors the
// secret_name_format CHECK constraint.
var secretRefPattern = regexp.MustCompile(`\$\{secret:([A-Za-z0-9_.-]+)\}`)

// ResolveSecretsJSON resolves ${secret:name} references inside the string
// leaves of a jsonb-encoded parameter value and returns the re-encoded JSON,
// the names of the resolved secrets (never their values), and an error if any
// reference cannot be resolved.
//
// If s does not contain the literal substring "${secret:" it is returned
// byte-identical with no parsing and no database round-trip (REQ-026).
// Otherwise s is decoded, every string leaf is scanned, and the result is
// re-encoded with encoding/json so that resolved values are correctly escaped
// (REQ-027).
func (pge *PgEngine) ResolveSecretsJSON(ctx context.Context, s string) (resolved string, names []string, err error)

// ResolveSecretsConnString resolves ${secret:name} references inside a libpq
// conninfo string, applying conninfo quoting to each resolved value per
// REQ-028. Same short-circuit contract as ResolveSecretsJSON.
func (pge *PgEngine) ResolveSecretsConnString(ctx context.Context, s string) (resolved string, names []string, err error)

// CheckSecretConfig logs an error when secrets exist but no encryption key is
// configured. It performs no query when pge.SecretEncryptionKey is non-empty
// (REQ-019/REQ-020).
func (pge *PgEngine) CheckSecretConfig(ctx context.Context) error

// resolveRefs is the shared engine: it scans s, calls timetable.resolve_secret
// once per match through pge.ConfigDb with pge.ClientName as the fixed client
// scope, and applies quote to each resolved value before substitution. The
// context passed to the query is marked with log.WithoutQueryArgs so that the
// encryption key never reaches the pgx tracer (REQ-018/REQ-030).
func (pge *PgEngine) resolveRefs(ctx context.Context, s string, quote func(value string, m []int, in string) string) (string, []string, error)
```

### 4.3 Go interfaces — `internal/log`

```go
// WithoutQueryArgs marks ctx so that PgxLogger.Log drops the "args" field
// from pgx tracer entries executed under it. Used for queries whose bound
// arguments carry secret material.
func WithoutQueryArgs(ctx context.Context) context.Context

// In PgxLogger.Log, before the level switch:
//   if data != nil && noQueryArgs(ctx) {
//       delete(data, "args")
//   }
```

### 4.4 Go interfaces — `internal/config/cmdparser.go`

```go
// New field on CmdOptions. The mapstructure tag is mandatory (REQ-016).
SecretEncryptionKey string `long:"secret-key" mapstructure:"secret-key" description:"Symmetric key used to decrypt timetable.secret values" env:"PGTT_SECRET_KEY"`
```

### 4.5 Call-site contract table

| File | Function | Change |
|---|---|---|
| `internal/scheduler/tasks.go` | `executeBuiltinTask` | `Debugf` logs parameter **count** only (REQ-032); no resolution here; `val` stays unresolved for both `f(...)` dispatch and `LogTaskExecution` (REQ-033) |
| `internal/scheduler/tasks.go` | `taskSendMail` | Receiver `_ *Scheduler` becomes `sch *Scheduler`; calls `sch.pgengine.ResolveSecretsJSON` before `json.Unmarshal` into `tasks.EmailConn` (REQ-034/REQ-036) |
| `internal/pgengine/transaction.go` | `ExecuteSQLCommand` | Resolves each `val` into `resolved`; unmarshals `resolved` into `params`; `executor.Exec` receives a `log.WithoutQueryArgs` context when any secret was substituted; `LogTaskExecution` receives unresolved `val` (REQ-037) |
| `internal/pgengine/transaction.go` | `ExecRemoteSQLTask` | Resolves `task.ConnectString` eagerly into a local before building the connection closure; `task.ConnectString` not mutated (REQ-038) |
| `internal/scheduler/shell.go` | `ExecuteProgramCommand` | Resolves each `val` into `resolved` for argv; `LogTaskExecution` receives unresolved `val` (REQ-039/REQ-031) |
| `internal/pgengine/access.go` | `LogTaskExecution` | No signature change; caller-side contract only (REQ-031) |
| `internal/log/log.go` | `PgxLogger.Log` | Drops the `args` field under a marked context (REQ-030) |
| `internal/pgengine/bootstrap.go` | — | No tracer change; redaction is context-scoped, not level-scoped |
| `internal/pgengine/secrets.go` | new file | `ResolveSecretsJSON`, `ResolveSecretsConnString`, `CheckSecretConfig`, `resolveRefs` |
| `main.go` | `run` | Calls `pge.CheckSecretConfig(ctx)` after the migration/upgrade block, before `scheduler.New`; also `dbapi = "00798"` |
| `internal/testutils/testcontainers.go` | `SetupPostgresContainerWithOptions` | Sets a fixed test `SecretEncryptionKey` (REQ-049) |
| `internal/tasks/mail.go` | — | No change (CON-003) |

### 4.6 Reference syntax grammar

```
reference := "${secret:" name "}"
name      := [A-Za-z0-9_.-]+
```

No whitespace inside the delimiters, no nesting, no default-value fallback,
case-sensitive exact match against `secret_name`.

## 5. Acceptance Criteria

- **AC-001**: Given a **fresh** database (no `timetable` schema) and a
  scheduler start with `SecretEncryptionKey` unset, When bootstrap completes,
  Then `timetable.secret`, `timetable.resolve_secret`,
  `timetable.secret_count`, and the `secret_touch` trigger all exist, and
  startup succeeds with no error and no behavior change versus pre-feature
  behavior.
- **AC-002**: Given a database migrated from the previous release, When
  `MigrateDb` runs, Then migration `00798` applies inside its transaction and
  produces objects identical to the fresh-install path; `TestMigrations`
  passes.
- **AC-003**: Given `main.go`, `migration.go`, `init.sql`, and the migration
  file name, Then all four agree on `00798`, and `dbapi` reported by
  `--version` equals the highest registered migration.
- **AC-004**: Given a database where `pgcrypto` was pre-installed into
  `public` before pg_timetable bootstrap, When the schema is created, Then
  `resolve_secret`'s `search_path` includes `public` and decryption succeeds.
- **AC-025**: Given a fresh install where `pgcrypto` resides in `timetable`,
  When `samples/Mail.sql` and `samples/RemoteDB.sql` are executed through
  `ExecuteCustomScripts` (a plain session with a default `search_path`), Then
  every `pgp_sym_encrypt` call succeeds. An unqualified call in that session
  fails with `function pgp_sym_encrypt(unknown, unknown) does not exist`, so
  this criterion fails if REQ-052 is not honored.
- **AC-005**: Given `timetable.secret` contains at least one row and
  `SecretEncryptionKey` is unset, When the scheduler starts, Then an error is
  logged at startup, before any chain executes.
- **AC-006**: Given `SecretEncryptionKey` is set, When the scheduler starts,
  Then `timetable.secret_count()` is not called.
- **AC-007**: Given a secret `smtp_main` for `client_name = 'worker-1'`, When
  a `SendMail` parameter contains `"password": "${secret:smtp_main}"` and the
  scheduler runs as `worker-1`, Then `tasks.SendMail` receives the correct
  plaintext password and the plaintext exists only in process memory.
- **AC-008**: Given a secret whose plaintext contains `"`, `\`, and a
  newline, When it is substituted into a jsonb parameter, Then the resolved
  JSON parses successfully and the unmarshalled field equals the original
  plaintext byte-for-byte.
- **AC-009**: Given a secret whose plaintext contains a space and a single
  quote, When it is substituted into `database_connection`, Then
  `pgx.ParseConfig` accepts the result and yields the original plaintext as
  the password. Given a template of the form `password='${secret:pw}'`, Then
  the delimiters are not doubled.
- **AC-010**: Given a secret `db_pw` scoped to `worker-2`, When a scheduler
  running as `worker-1` references `${secret:db_pw}`, Then resolution fails
  with an error naming the secret and the attempted client scope, the task
  does not execute, and the error is indistinguishable from the
  nonexistent-secret case.
- **AC-011**: Given `SecretEncryptionKey` is empty and a parameter contains
  `${secret:x}`, When resolution runs, Then it fails naming the missing
  configuration and issues **zero** queries.
- **AC-012**: Given a wrong `SecretEncryptionKey` and an existing secret,
  When resolution runs, Then the `Wrong key or corrupt data` error is
  surfaced wrapped with the secret name and is not reported as "not found".
- **AC-013**: Given any builtin, SQL, remote, or PROGRAM execution that
  consumed a reference, When the row lands in
  `timetable.execution_log.params`, Then that column contains the literal
  `${secret:...}` reference string, never the plaintext. This MUST be
  asserted for all three `LogTaskExecution` call sites, including
  `internal/scheduler/shell.go`.
- **AC-014**: Given `--log-level=debug --log-database-level=debug` and a SQL
  task that consumed a reference, When `timetable.log` and the stdout log are
  inspected, Then no entry contains the plaintext value, no entry contains
  the encryption key, and the `Query` entries for those statements carry no
  `args` field while retaining `sql`.
- **AC-015**: Given a builtin task with parameters, When the debug log is
  inspected, Then the `executeBuiltinTask` entry contains a parameter count
  and no parameter values.
- **AC-016**: Given a role other than the object owner with no explicit
  grants, When it attempts `SELECT` on `timetable.secret` or calls
  `resolve_secret` or `secret_count`, Then all three fail with
  permission-denied.
- **AC-017**: Given a parameter or connection string containing no
  `${secret:` substring, When the resolvers are called, Then the input is
  returned byte-identical with zero database round-trips, verified by a
  query-count assertion rather than output equality alone.
- **AC-018**: Given an existing secret row, When it is `UPDATE`d, Then
  `updated_at` advances and `updated_by` reflects the updating
  `session_user`.
- **AC-019**: The system shall reject any `secret_name` failing
  `^[A-Za-z0-9_.-]+$` with a constraint violation at `INSERT`/`UPDATE`.
- **AC-020**: Given an `INSERT` omitting `client_name` or passing `NULL`,
  Then it fails with a `NOT NULL` violation and no row is inserted.
- **AC-021**: Given secrets named `smtp_main` under both `worker-1` and
  `worker-2` holding different plaintexts, When
  `resolve_secret('smtp_main', 'worker-1', key)` is called, Then only
  `worker-1`'s value is returned.
- **AC-022**: Given a `CmdOptions` parsed from `--secret-key=k` and,
  separately, from `PGTT_SECRET_KEY=k`, Then `SecretEncryptionKey == "k"` in
  both cases. This asserts the `mapstructure` tag of REQ-016 and fails
  without it.
- **AC-023**: Given `samples/Mail.sql` and `samples/RemoteDB.sql` after
  migration, When `TestSamplesScripts` and `TestRun` execute them against a
  fresh container with no manual setup, Then both pass, the secret rows are
  created, the `SendMail` parameter reads `"${secret:smtp_main}"`,
  `database_connection` contains `password=${secret:remotedb_demo}`, and the
  `-- Legacy (deprecated):` comment is present in `Mail.sql`.
- **AC-024**: Given a chain created before this feature with a literal
  password in `parameter.value`, When it runs after the migration, Then
  behavior is unchanged.

## 6. Test Automation Strategy

- **Test Levels**:
  - *Unit* — `internal/pgengine` (resolver short-circuit, JSON escaping,
    conninfo quoting, error classes), `internal/log` (args redaction),
    `internal/config` (flag/env binding).
  - *Integration* — Go tests against a real PostgreSQL via
    `testcontainers-go`, using the existing
    `testutils.SetupPostgresContainer` helper (`postgres:18-alpine`).
  - No new end-to-end layer; `TestSamplesScripts` and `TestRun` already
    exercise the sample smoke path.
- **Frameworks**: `testing` plus `github.com/stretchr/testify`
  (`assert`/`require`); `github.com/pashagolub/pgxmock/v5` for
  `PgxPoolIface` expectations where a live database is unnecessary — this is
  how AC-017's zero-round-trip claim is proven, via unmet-expectation
  assertions; `testcontainers-go` + `testcontainers-go/modules/postgres` for
  schema, trigger, grant, and decryption tests.
- **Named tests to add**:
  - `TestResolveSecretsShortCircuit` (AC-017, pgxmock).
  - `TestResolveSecretsJSONEscaping` (AC-008).
  - `TestResolveSecretsConnStringQuoting` (AC-009).
  - `TestResolveSecretsErrorClasses` (AC-010, AC-011, AC-012).
  - `TestSecretSchemaFreshInstall` (AC-001, AC-004, AC-018, AC-019, AC-020,
    AC-021).
  - `TestSecretGrants` (AC-016), creating a throwaway role inside the test.
  - `TestExecutionLogNeverContainsPlaintext` (AC-013), covering SQL, builtin,
    and PROGRAM paths.
  - `TestPgxTracerRedactsSecretArgs` (AC-014), asserting on `timetable.log`
    contents after a debug-level run.
  - `TestSecretKeyConfigBinding` (AC-022) in `internal/config`.
  - `TestMigrations` extended for `00798` (AC-002, AC-003).
  - `TestSecretStartupCheck` (AC-005, AC-006) — asserts the error is logged
    when secrets exist without a key, and that `secret_count()` is not
    queried when a key is present (pgxmock for the negative case).
  - `TestSendMailResolvesSecret` (AC-007) — `taskSendMail` end to end against
    a container, with a stub SMTP listener or an injected `tasks.SendMail`
    boundary, asserting the plaintext password reaches `EmailConn`.
  - `TestBuiltinDebugLogOmitsParamValues` (AC-015) — captures logrus output
    from `executeBuiltinTask` and asserts a count is present and no
    parameter value is.
  - `TestSamplesScripts` and `TestRun`, unmodified in name, extended by the
    REQ-049 harness change (AC-023, AC-025).
  - `TestLegacyLiteralParametersUnchanged` (AC-024) — a chain whose
    `parameter.value` holds a literal password executes identically after the
    migration.
- **Test Data Management**: Integration tests create and drop their own
  `timetable.secret` rows with explicit cleanup, or reset with
  `DROP SCHEMA IF EXISTS timetable CASCADE` followed by re-bootstrap, as
  `migration_test.go` already does. No shared fixture secrets. The single
  shared constant is the test encryption key set by `testutils` (REQ-049).
- **CI/CD Integration**: New tests run under the existing
  `.github/workflows/build.yml` job (`go test -failfast -v -timeout=300s
  -coverprofile=profile.cov ./...`), which also runs `golangci-lint`. No new
  workflow file. Note the 300 s suite timeout — the added integration tests
  MUST reuse the existing container helpers rather than starting additional
  containers per test case.
- **Coverage Requirements**: CI uploads `profile.cov` to Coveralls and
  enforces **no numeric threshold**; do not claim one. The requirement is
  behavioral instead: every AC above MUST map to at least one named test.
- **Performance Testing**: None. AC-017's query-count assertion is the only
  performance-relevant check, guarding REQ-026/CON-002.

## 7. Rationale & Context

- **Why `pgcrypto` over plain `TEXT`**: it raises the bar above "any `SELECT`
  yields plaintext" without promising KMS-grade protection, which is
  explicitly out of scope. The honest security claim is SEC-001's: the key
  lives outside the database, so a `pg_dump` or a logical replica alone is
  insufficient.
- **Why `client_name NOT NULL`, diverging from `timetable.chain`**: the
  column name and type are copied from `timetable.chain.client_name` for
  consistency, but the nullable "any client" convenience that makes sense for
  *routing* a chain does not make sense for *authorizing decryption*. Every
  scheduler process already has exactly one mandatory identity, so a
  `NULL`-scoped secret would be decryptable by every worker that can reach
  the database — reintroducing the whole-catalog exposure the scoping column
  exists to bound.
- **Why no surrogate `secret_id`**: a secret is only ever addressed by its
  `${secret:name}` reference; no code path resolves one by numeric id and no
  table holds a foreign key to it. The natural composite key is sufficient,
  and a `BIGSERIAL` would be an unused column.
- **Why no new roles**: pg_timetable has never created or managed roles, and
  the migrator's single-transaction apply turns a `GRANT` to a nonexistent
  role into a permanent startup failure. Ownership by the schema-creating
  role plus `REVOKE ... FROM PUBLIC` achieves the actual goal — keeping every
  other role away from ciphertext and decryption — without inventing
  infrastructure the project does not own. SEC-001 states the resulting
  property truthfully rather than claiming that no query anywhere can reach
  plaintext.
- **Why the extension schema is baked into the function body**: pinning
  `search_path` is required for a `SECURITY DEFINER` function, but pinning it
  to a fixed list breaks whenever `pgcrypto` already exists in another schema.
  Generating the body with the real schema interpolated is deterministic on
  both fresh installs and pre-existing databases, and — unlike a post-hoc
  `ALTER FUNCTION` — survives PostgreSQL's create-time validation of
  `LANGUAGE sql` bodies (REQ-008).
- **Why masking is not a follow-up**: without it the feature is a compliance
  checkbox that fails audits. Three concrete leak paths exist in the code
  today — `execution_log.params` persisting the raw parameter string, the
  builtin debug log printing `%+q` of all parameter values, and the pgx
  tracer writing bound arguments into `timetable.log` at debug level. The
  third is the most severe because it persists to the database, and it lives
  in files a naive implementation would never touch.
- **Why the pgx tracer fix is context-scoped rather than level-scoped**:
  lowering the tracer's level would remove legitimate diagnostics for all
  queries. Marking only the contexts of calls that carry secret material
  keeps everything else observable, and pgx guarantees the caller's context
  reaches `Logger.Log`.
- **Why resolution happens inside `taskSendMail` and not in
  `executeBuiltinTask`**: the loop variable `val` is passed both to the
  builtin and to `LogTaskExecution` on the next line. Rebinding it to the
  resolved form would write plaintext to `execution_log` — the exact defect
  this feature exists to fix.
- **Why jsonb walking instead of flat substitution**: a realistic password
  containing `"` or `\` would otherwise corrupt the parameter document and
  fail the downstream unmarshal, turning a working credential into an opaque
  parse error.
- **Why PROGRAM is in scope despite the argv caveat**: excluding it would
  pass the literal `${secret:x}` to the child process — silently wrong rather
  than loudly unsupported. GUD-001 records the preferred future design and
  SEC-003 documents the present exposure.
- **Why references are excluded from SQL command text**: command text is
  logged verbatim by design across the product; embedding secrets there is an
  anti-pattern this feature does not accommodate.
- **Why the samples must remain self-contained**: `TestSamplesScripts`
  executes every file in `samples/` against a fresh container with no manual
  setup. A sample that requires an operator to pre-insert a secret or match a
  placeholder client name converts a green test into a red one, so the sample
  migration and the harness change ship together.
- **Verification provenance of §4.1**: the SQL block in §4.1 was executed
  verbatim against a scratch PostgreSQL 16.1 cluster during authoring. All of
  the following were observed rather than assumed, and any implementation
  divergence should be re-checked the same way:
  - the whole block applies cleanly against a database containing only
    `CREATE SCHEMA timetable` (pgcrypto absent → installed into `timetable`)
    and, separately, against a database where `pgcrypto` pre-existed in
    `public`; in the latter `pg_proc.prosrc` contains
    `public.pgp_sym_decrypt` and decryption succeeds;
  - the earlier `ALTER FUNCTION ... SET search_path` formulation was rejected
    by exactly this test — it fails at `CREATE FUNCTION` time in the
    pre-existing-extension case (REQ-053);
  - `provolatile='s'`, `proisstrict=true`, `prosecdef=true` for
    `resolve_secret`; `prosecdef=true` for `secret_count`;
  - `has_function_privilege('public', ...)` is false for both functions and
    `has_table_privilege('public', ...)` is false for the table;
  - a missing secret yields **one row containing NULL**, and a wrong key
    raises `ERROR: Wrong key or corrupt data` with
    `CONTEXT: SQL function "resolve_secret" statement 1`;
  - the `secret_touch` trigger overrides a deliberately falsified
    `updated_at='epoch', updated_by='liar'` UPDATE;
  - both `secret_name_format` violations (`'has space'`, `''`) and a NULL
    `client_name` are rejected;
  - a value encrypted from `''` round-trips to `''`;
  - a fresh unprivileged role is refused all three accesses (as
    `permission denied for schema timetable`, since it also lacks `USAGE`);
  - the owning role *can* `SELECT value_enc`, which is precisely why SEC-001
    states the key — not the grant model — as the confidentiality boundary.

## 8. Dependencies & External Integrations

### External Systems

- None. The feature is entirely intra-Postgres and intra-process and adds no
  network dependency.

### Third-Party Services

- None.

### Infrastructure Dependencies

- **INF-001**: PostgreSQL server hosting the `timetable` schema, able to load
  `pgcrypto`. Since PostgreSQL 13 `pgcrypto` is a **trusted** extension,
  installable by a non-superuser holding `CREATE` on the database, so managed
  services (RDS, Azure, Cloud SQL, Supabase and similar) satisfy this without
  superuser. Only PostgreSQL 12 and older require superuser or an
  allowlist entry.
- **INF-002**: `pgcrypto` requires a PostgreSQL build with OpenSSL support.
  Builds without it cannot host this feature; the migration fails loudly
  (CON-001) rather than degrading.

### Data Dependencies

- **DAT-001**: `timetable.secret` rows are provisioned exclusively by
  `INSERT`/`UPDATE` from an authorized session. No external feed or import
  format is introduced.

### Technology Platform Dependencies

- **PLT-001**: No new Go module dependency. `pgcrypto` is server-side; the
  redaction marker uses only `context` and the existing `tracelog`
  integration.
- **PLT-002**: The trigger uses `EXECUTE PROCEDURE` and plain
  `CREATE TRIGGER` so that no PostgreSQL version in the project's supported
  matrix is dropped (`EXECUTE FUNCTION` requires PG11+,
  `CREATE OR REPLACE TRIGGER` requires PG14+).

### Compliance Dependencies

- **COM-001**: This feature raises the bar against other database roles,
  logical replicas, and `pg_dump` output taken without the key, but does not
  by itself satisfy any specific regulatory secret-management control (PCI-DSS
  key custody, SOC 2 rotation). Documentation (REQ-050, SEC-002) must state
  this boundary explicitly to prevent compliance-checkbox misuse.

## 9. Examples & Edge Cases

```sql
-- Admin inserts a secret scoped to the client that will resolve it.
-- client_name must equal that worker's own -c/--clientname value; there is
-- no global/NULL-scoped secret.
-- pgp_sym_encrypt is schema-qualified because pgcrypto lives in `timetable`
-- on fresh installs and is therefore not on a default session search_path
-- (REQ-052).
INSERT INTO timetable.secret (client_name, secret_name, value_enc)
VALUES ('worker-1', 'smtp_main', timetable.pgp_sym_encrypt('s3cr3t pw''s', 'the-configured-key'));

-- The same secret_name under a different client is an independent secret,
-- not another version of the one above.
INSERT INTO timetable.secret (client_name, secret_name, value_enc)
VALUES ('worker-2', 'smtp_main', timetable.pgp_sym_encrypt('other-pw', 'the-configured-key'));

-- Overwrite in place; the secret_touch trigger refreshes updated_at/updated_by.
UPDATE timetable.secret
   SET value_enc = timetable.pgp_sym_encrypt('rotated-by-hand', 'the-configured-key')
 WHERE client_name = 'worker-1' AND secret_name = 'smtp_main';

-- Optional operator step: delegate writes to a separate role that the
-- operator already manages. Not performed by the schema (REQ-014).
-- GRANT INSERT, UPDATE, DELETE ON timetable.secret TO my_secret_admin;

-- Task parameter referencing the worker-1-scoped secret:
-- { "username": "svc@example.com", "password": "${secret:smtp_main}" }

-- database_connection referencing a secret inline:
-- host=remote.example.com port=5432 dbname=app user=svc password=${secret:remote_db_pw}
```

Escaping examples:

```text
# jsonb leaf, secret value is:  he said "hi"\then
input   {"password":"${secret:p}"}
output  {"password":"he said \"hi\"\\then"}          # valid JSON, unmarshals to the original

# conninfo, secret value is:  s3cr3t pw's
input   host=h dbname=d password=${secret:p}
output  host=h dbname=d password='s3cr3t pw\'s'      # quoted because it contains a space

# conninfo, reference already delimited
input   host=h password='${secret:p}'
output  host=h password='s3cr3t pw\'s'               # delimiters not doubled

# conninfo, secret value has no special characters
input   host=h password=${secret:p}
output  host=h password=simplepw                     # no quoting added
```

Edge cases:

- **Unknown name**: `${secret:does_not_exist}` → one row containing NULL →
  error `secret "does_not_exist" not found for client "<client_name>"`; task
  fails.
- **Name exists under another client**: identical error class, deliberately
  indistinguishable so existence does not leak across clients (REQ-044).
- **Malformed reference**: `${secret:}` or `${secret:has space}` does not
  match the pattern and is passed through as literal text. No special error
  path; the character class is the single source of truth.
- **No `${secret:` substring**: returned byte-identical, zero queries — this
  also preserves the existing behavior for parameter values that are invalid
  JSON, since nothing is parsed.
- **Key unset with a reference present**: fails before any query, naming the
  missing configuration (REQ-041 class 2); the startup error of REQ-020 is
  the earlier warning signal.
- **Wrong key**: `pgp_sym_decrypt` raises; wrapped with the secret name
  (REQ-041 class 3), never reported as "not found".
- **Multiple references in one string**: e.g. `user=${secret:db_user}` and
  `password=${secret:db_pw}` — each resolves independently and `names`
  contains both.
- **Resolved value looks like a reference**: a secret whose plaintext is
  literally `${secret:other}` is inserted verbatim and never re-scanned
  (REQ-024).
- **Empty secret value**: `pgp_sym_encrypt('', key)` is legal and resolves to
  the empty string. In conninfo it MUST be emitted as `''` (REQ-028), because
  a bare `password=` followed by whitespace would swallow the next token.
- **`NULL`/omitted `client_name` on insert**: rejected by the `NOT NULL`
  constraint of the composite primary key. No code path, application-level or
  SQL, can create a global secret.
- **`pgcrypto` already installed in `public`**: detected and appended to
  `resolve_secret`'s `search_path`; no attempt is made to relocate the
  extension.
- **Debug logging enabled**: the `Query` entries for secret-bearing
  statements retain `sql` and lose `args`; every other query keeps both.

## 10. Validation Criteria

- All acceptance criteria in §5 (AC-001 … AC-024) pass under §6's strategy,
  each mapped to a named test.
- `go vet` and the CI `golangci-lint` run pass on all new and modified files
  with no new suppressions.
- Fresh-install and migration paths converge: a database bootstrapped from
  `ddl.sql` and a database upgraded through `00798.sql` yield identical
  definitions for `timetable.secret`, its constraint, its trigger, and both
  functions (comparable via `pg_catalog` introspection).
- `00798` appears consistently in the migration file name,
  `internal/pgengine/migration.go`, `internal/pgengine/sql/init.sql` (id 18),
  and `main.go`'s `dbapi`.
- Grant verification: a throwaway role with no explicit grants can neither
  `SELECT timetable.secret` nor `EXECUTE` either new function; the owning
  role can do both. Documentation states the SEC-001 property rather than an
  absolute no-plaintext-anywhere claim.
- Leak verification, after a debug-level run of a chain that consumes a
  secret through builtin, SQL, remote, and PROGRAM paths:
  - `SELECT params FROM timetable.execution_log` contains only reference
    forms.
  - `SELECT message, message_data FROM timetable.log` contains neither the
    plaintext nor the encryption key.
  - the stdout/file log contains neither.
- `samples/Mail.sql` and `samples/RemoteDB.sql` execute end to end against a
  fresh migrated database under `TestSamplesScripts` and `TestRun` with no
  manual setup, and the resolved secret is actually used.
- Backward compatibility: a chain whose `parameter.value` holds a literal
  password behaves exactly as before.

## 11. Related Specifications / Further Reading

This specification is self-contained; the items below are code and external
references, not prerequisites.

- `internal/pgengine/sql/ddl.sql` — source of the `client_name` scoping
  pattern (PAT-001) and the fresh-install target of REQ-045.
- `internal/pgengine/migration.go`, `internal/pgengine/sql/init.sql`,
  `main.go` — the three-file migration registration convention (REQ-046).
- `internal/pgengine/access.go`, `internal/pgengine/transaction.go`,
  `internal/scheduler/tasks.go`, `internal/scheduler/shell.go`,
  `internal/log/log.go`, `internal/pgengine/bootstrap.go` — the masking
  surfaces of REQ-030 … REQ-035.
- `internal/config/cmdparser.go`, `internal/config/config.go` — the
  configuration surface of REQ-015/REQ-016.
- `internal/testutils/testcontainers.go`, `internal/pgengine/migration_test.go`,
  `internal/pgengine/pgengine_test.go`, `internal/scheduler/scheduler_test.go`
  — the test harness of §6 and REQ-049.
- PostgreSQL documentation, "F.26. pgcrypto — cryptographic functions"
  (<https://www.postgresql.org/docs/current/pgcrypto.html>) — trusted-extension
  status (INF-001) and `pgp_sym_encrypt`/`pgp_sym_decrypt` semantics.
- `docs/samples.md`, `docs/yaml-usage-guide.md`, `docs/database_schema.md` —
  documentation targets of REQ-050/REQ-051.
