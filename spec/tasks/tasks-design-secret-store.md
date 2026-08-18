---
description: "Task list for implementing the Postgres-native secret store (timetable.secret)"
---

# Tasks: Postgres-Native Secret Store (`timetable.secret`)

**Input**: `spec/spec-design-secret-store.md` (v2.1)
**Prerequisites**: that spec is self-contained; no other design document is required.

**Tests**: Tests ARE requested. The specification mandates them explicitly — §6
names every test to add, and §10 requires that all 27 acceptance criteria
(AC-001 … AC-027) map to at least one named test. Test tasks below are
therefore REQUIRED, not optional.

**Non-negotiable product rule (v2.1)**: pg_timetable MUST NOT install or
require any PostgreSQL extension. `pgcrypto` is an **optional** dependency of
the secret store, provisioned by whoever deploys the database. No
`CREATE EXTENSION` may appear anywhere under `internal/pgengine/sql/`, and a
database without `pgcrypto` MUST bootstrap, migrate, start, and run every
non-secret chain exactly as before (REQ-007, REQ-053, REQ-054, CON-001).
Samples MAY install it — they are demos a user runs deliberately, not product
DDL (REQ-052). Tasks re-opened below (`[ ]`) are the ones whose earlier
`[x]` outcome encoded the rejected "pg_timetable installs pgcrypto" premise.

**Organization**: Tasks are grouped by user story. Each story is a shippable
increment that leaves the repository green.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1 … US4)
- Every task names exact file paths and the requirement/AC IDs it satisfies

**Requirement traceability**: every task cites `REQ-`/`SEC-`/`CON-`/`AC-` IDs
from the spec. A task is not done until its cited criteria hold.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Establish the baseline and close the one open design decision
before any code changes.

- [x] T001 Record the pre-change baseline: run `go test ./...` and save the
      result. Every later phase must leave the suite at least as green. Note
      that `internal/pgengine/pgengine_test.go` (`TestSamplesScripts`) and
      `internal/scheduler/scheduler_test.go` (`TestRun`) currently pass and
      will be affected by Phase 6 (REQ-049).
- [x] T002 Decide and record the REQ-049 sample self-containment mechanism —
      the spec deliberately leaves this open. Choose one:
      (a) samples derive the client from
      `current_setting('pg_timetable.current_client_name', true)`, or
      (b) samples use a literal placeholder and
      `internal/testutils/testcontainers.go` seeds a matching secret.
      The chosen mechanism MUST make `samples/*.sql` runnable by
      `TestSamplesScripts`, which executes them with no manual setup. Write
      the decision into the header comment of `samples/Mail.sql` so it is
      discoverable at the point of use. Note that `pgcrypto` acquisition for
      the samples is settled, not open: each sample issues its own
      `CREATE EXTENSION IF NOT EXISTS pgcrypto` (REQ-047, REQ-048, REQ-052).
- [x] T003 [P] Fix the migration number. Confirm the highest registered
      migration in `internal/pgengine/migration.go` is still `00797`; if
      another migration has landed, use the next free number and apply it
      consistently to the migration file name, the `migration.go` entry, the
      `internal/pgengine/sql/init.sql` seed row, and `main.go`'s `dbapi`
      (REQ-046, AC-003). All four MUST agree.
- [x] T004 [P] Confirm the local verification path for SQL work: either
      Docker (for `testcontainers-go`) or a local PostgreSQL instance. The
      §4.1 SQL of v2.1 is a NEW formulation (plpgsql `resolve_secret`, no
      `CREATE EXTENSION`) and MUST be re-executed against a live server in
      three extension scenarios during Phase 2: absent, present in `public`,
      relocated to a private schema (AC-004, AC-025, AC-026).

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Schema, configuration, redaction plumbing, and the resolver. No
story can resolve or mask a secret until this phase is complete.

**⚠️ CRITICAL**: No user story work can begin until this phase is complete.

### Schema and migration

- [x] T005 Rewrite the schema block in `internal/pgengine/sql/ddl.sql` to the
      v2.1 §4.1 form: the `timetable.secret` table with
      `PRIMARY KEY (client_name, secret_name)` and no surrogate id, the
      `secret_name_format` CHECK, all five `COMMENT`s,
      `REVOKE ALL ... FROM PUBLIC`, `timetable.secret_touch()` + the
      `secret_touch` `BEFORE UPDATE` trigger, `timetable.resolve_secret`, and
      `timetable.secret_count` (REQ-001 … REQ-014, SEC-005, SEC-006, PAT-001,
      PLT-002, CON-001, CON-007, DAT-001).
      Two things MUST be deleted from the current WIP content:
      - the `DO $$ ... CREATE EXTENSION pgcrypto SCHEMA timetable ... $$`
        acquisition block — pg_timetable never installs an extension, and the
        DDL MUST apply cleanly to a database that has no `pgcrypto` and whose
        role could not install one (REQ-007, CON-001);
      - the `DO $OUTER$ ... EXECUTE format($SQL$ CREATE OR REPLACE FUNCTION
        ... LANGUAGE sql ... %I.pgp_sym_decrypt ... $SQL$) ... $OUTER$`
        generation of `resolve_secret`, replaced by a plain
        `CREATE OR REPLACE FUNCTION ... LANGUAGE plpgsql` that looks the
        extension up in its own body.
      Critical details that are easy to get wrong:
      - `resolve_secret` MUST be `LANGUAGE plpgsql`. `LANGUAGE sql` is
        PROHIBITED: PostgreSQL resolves a `LANGUAGE sql` body at
        `CREATE FUNCTION` time, so it would fail to create — and thus fail the
        migration and block startup — on any database without `pgcrypto` on
        the pinned `search_path`. The plpgsql validator checks syntax only and
        never resolves referenced functions, and the decrypt call additionally
        lives inside `EXECUTE format(...)` dynamic SQL, which the validator
        never inspects (REQ-008, REQ-053).
      - Order inside `resolve_secret` matters: fetch the row first and
        `RETURN NULL` when `NOT FOUND`, **then** look up the extension schema.
        A nonexistent secret must report not-found even with no `pgcrypto`
        installed (REQ-008, REQ-054).
      - The missing-extension path MUST
        `RAISE EXCEPTION ... USING ERRCODE = 'feature_not_supported'` (`0A000`)
        naming `pgcrypto`, plus a `HINT`. It MUST NOT be a silent NULL and
        MUST NOT be conflated with not-found (REQ-041 class 4).
      - Use `EXECUTE PROCEDURE` and plain `CREATE TRIGGER`, not
        `EXECUTE FUNCTION` / `CREATE OR REPLACE TRIGGER` (PLT-002).
      - Reference no role name (REQ-009). A `GRANT` to a nonexistent role
        aborts the whole migration transaction and blocks startup.
- [x] T006 Apply the same rewrite to
      `internal/pgengine/sql/migrations/00798.sql` so both files again hold
      identical object definitions — likewise with no `CREATE EXTENSION`,
      which matters most here: the migrator wraps each migration in one
      transaction, so an extension failure inside it would permanently block
      startup (REQ-007, REQ-045, PAT-002). The migration alone is insufficient
      and the DDL alone is insufficient: `ExecuteSchemaScripts` runs `ddl.sql`
      only when the `timetable` schema is absent, while `init.sql` seeds
      `timetable.migration` through the current release, so a fresh database
      never runs new migrations.
- [x] T007 Register the migration in all three places, per the in-code comment
      in `internal/pgengine/migration.go`: the appended
      `&migrator.Migration{Name: "00798 Add timetable.secret store", ...}`
      entry, the `(18, '00798 Add timetable.secret store')` row in
      `internal/pgengine/sql/init.sql`, and `dbapi = "00798"` in `main.go`
      (REQ-046, AC-003).
- [x] T008 Verify the schema against a live server in ALL THREE extension
      scenarios before proceeding:
      (a) `pgcrypto` **absent** → the whole block still applies, both
      functions are created, `secret_count()` returns 0, a scheduler starts
      with no error and no warning, `resolve_secret` on an unknown name
      returns NULL, and `resolve_secret` on an existing row raises SQLSTATE
      `0A000` (AC-025, AC-027, REQ-053, REQ-054);
      (b) `pgcrypto` in **`public`** → an inserted value decrypts, even though
      `public` is not on the function's pinned `search_path` (AC-004);
      (c) after `ALTER EXTENSION pgcrypto SET SCHEMA ext` → the same value
      still decrypts (AC-026).
      Also confirm a wrong key raises `Wrong key or corrupt data`. Do NOT
      reuse the v2.0 verification notes for the SQL itself: the
      `CREATE EXTENSION` block, the `LANGUAGE sql` body, and the create-time
      `EXECUTE format(...)` generation are all superseded.

### Configuration

- [x] T009 [P] Add `SecretEncryptionKey` to `CmdOptions` in
      `internal/config/cmdparser.go` with all three tags:
      `long:"secret-key" mapstructure:"secret-key" env:"PGTT_SECRET_KEY"`.
      The `mapstructure` tag is load-bearing, not cosmetic: `NewConfig` binds
      flags into viper by long name and then calls `v.Unmarshal`, which
      matches `secret-key` to the field only through that tag. Copy the
      `NoProgramTasks` field as the pattern — NOT `ClientName`/`ConnStr`,
      which work only incidentally because their flag names already match
      their field names (REQ-015, REQ-016, PAT-003).

### Log redaction plumbing

- [x] T010 [P] Add `WithoutQueryArgs(ctx context.Context) context.Context` and
      a private `noQueryArgs(ctx)` predicate to `internal/log/log.go`, using
      an unexported context-key type consistent with the existing
      `loggerKey struct{}` (REQ-030).
- [x] T011 In `PgxLogger.Log` (`internal/log/log.go`), delete the `args` key
      from `data` when the context is marked, before the fields are attached
      to the logger. Retain `sql` — command text is logged verbatim by design
      (REQ-023, REQ-030). This is the fix for the most severe leak: at
      `--log-level=debug` the pgx tracer logs every query's bound arguments,
      `tracelog.logQueryArgs` truncates but does not redact, and those entries
      are persisted into the `timetable.log` table by `LogHook.send`.
- [x] T012 Fix the confirmed standalone defect in
      `internal/scheduler/tasks.go` (`executeBuiltinTask`): replace
      `Debugf("Executing builtin task with parameters %+q", paramValues)` with
      a parameter **count** only (REQ-032). This task is independently
      shippable — it is a real leak today, before any secret store exists, and
      may be merged on its own.

### Resolver

- [x] T013 Create `internal/pgengine/secrets.go` with `secretRefPattern =
      regexp.MustCompile(`\$\{secret:([A-Za-z0-9_.-]+)\}`)` and the shared
      `resolveRefs` engine: fixed client scope `pge.ClientName`, one
      `timetable.resolve_secret` call per match through `pge.ConfigDb`, no
      recursion into resolved values, and the query context wrapped in
      `log.WithoutQueryArgs` so the encryption key never reaches the tracer
      (REQ-018, REQ-021, REQ-022, REQ-024, REQ-025, REQ-030, REQ-040,
      GUD-002). Add no extension probe of any kind — not at startup, not per
      task, not per resolution. The only place `pgcrypto` is looked for is
      inside `resolve_secret`'s own body (REQ-007, REQ-054).
- [x] T014 Implement the mandatory short-circuit in `resolveRefs`: if the
      input lacks the literal substring `${secret:`, return it byte-identical
      with no JSON parsing, no regexp evaluation, and no database round-trip.
      This is a correctness guarantee, not an optimization — it preserves
      existing behavior (including existing malformed-JSON error paths) for
      every parameter that uses no secrets (REQ-026, CON-002, AC-017).
- [x] T015 Implement `ResolveSecretsJSON` in
      `internal/pgengine/secrets.go`: decode the parameter, substitute inside
      **string leaves only**, and re-encode with `encoding/json` so escaping
      is handled. Flat substitution on raw jsonb text is PROHIBITED — a
      password containing `"`, `\`, or a newline would corrupt the document
      and break the downstream `json.Unmarshal` (REQ-027, REQ-029, AC-008).
- [x] T016 Implement `ResolveSecretsConnString` in
      `internal/pgengine/secrets.go` with libpq conninfo quoting: wrap in
      single quotes and backslash-escape `\` and `'` when the value is empty
      or contains whitespace, `'`, or `\`; omit the wrapping when the
      reference is already delimited by single quotes in the template so the
      delimiters are not doubled. An empty value MUST emit `''`, because a
      bare `password=` would swallow the next token (REQ-028, REQ-029,
      AC-009).
- [x] T017 Implement the **four** distinguished failure classes in
      `internal/pgengine/secrets.go` (REQ-041, REQ-042, REQ-043, REQ-044,
      REQ-054). Classes 1–3 already exist in the WIP; class 4 is new:
      1. **Missing secret** — scan into a nullable target (`*string` or
         `pgtype.Text`) and treat NULL as not found. Do NOT rely on
         `pgx.ErrNoRows`; `resolve_secret` returns one row containing NULL, so
         `ErrNoRows` never occurs on this path. Error text must name the
         secret and the client scope, and must be identical for "exists under
         another client" so existence does not leak.
      2. **Key unset** — fail before issuing any query when a reference is
         present and `SecretEncryptionKey` is empty
         (`pgp_sym_encrypt(x, '')` is legal, so an empty key otherwise yields
         a confusing corrupt-data error).
      3. **Wrong key** — wrap the `Wrong key or corrupt data` error with the
         secret name; never report it as not-found.
      4. **`pgcrypto` absent** — detect `*pgconn.PgError` with
         `Code == "0A000"` (`feature_not_supported`, raised by
         `resolve_secret`) and wrap it with the secret name plus a statement
         that the secret store needs the `pgcrypto` extension and that
         installing it is the database administrator's responsibility. It MUST
         NOT be reported as class 1 or class 3, MUST NOT escalate beyond the
         referencing task, and MUST NOT trigger any retry, startup failure, or
         feature-wide disablement.
      Silent empty-string substitution is PROHIBITED in all four.
- [x] T018 Implement `CheckSecretConfig` in `internal/pgengine/secrets.go` and
      call it from `run()` in `main.go`, positioned after the
      migration/upgrade block (so the schema is known current) and before
      `scheduler.New`. It MUST return immediately without querying when
      `SecretEncryptionKey` is non-empty, and otherwise call
      `timetable.secret_count()` exactly once and log an error when the count
      exceeds zero. A failure of the check itself is logged, never fatal.
      `secret_count()` needs no `pgcrypto`, so this check works unchanged on a
      database without the extension, and it MUST NOT be extended into an
      extension probe or emit any extension-related diagnostic
      (REQ-013, REQ-019, REQ-020, REQ-054, CON-002).

### Foundational tests

- [x] T019 [P] `TestSecretSchemaFreshInstall` in
      `internal/pgengine/secrets_test.go` (package `pgengine_test`, using
      `testutils.SetupPostgresContainer`): asserts table/functions/trigger
      exist, the `secret_touch` trigger overrides a falsified
      `updated_at='epoch', updated_by='liar'` UPDATE, `secret_name_format`
      rejects `'has space'` and `''`, NULL `client_name` is rejected, and
      per-client isolation holds (AC-001, AC-018, AC-019, AC-020, AC-021).
      Re-opened because the WIP version relies on product DDL having installed
      `pgcrypto` and on `timetable.pgp_sym_encrypt` existing: the test MUST now
      install the extension itself in its own fixture and call
      `pgp_sym_encrypt` from wherever `CREATE EXTENSION` put it (REQ-049).
- [x] T019a [P] `TestResolveSecretLocatesPgcrypto` in
      `internal/pgengine/secrets_test.go`: install `pgcrypto` (landing in
      `public`), store and resolve a secret, then
      `CREATE SCHEMA ext; ALTER EXTENSION pgcrypto SET SCHEMA ext;` and
      resolve the same secret again. Both MUST succeed, proving the schema is
      discovered at call time rather than pinned (AC-004, AC-026, REQ-008).
- [x] T019b [P] `TestSecretsWithoutPgcrypto` in
      `internal/pgengine/secrets_test.go`: on a container where `pgcrypto` is
      NOT installed, assert bootstrap/migration succeeded, both functions and
      the table exist, `secret_count()` returns 0, `resolve_secret` on an
      unknown name returns NULL, and — after inserting a row whose `value_enc`
      is a plain bytea literal rather than `pgp_sym_encrypt` output — that
      resolving it fails with SQLSTATE `0A000` wrapped with the secret name,
      while the scheduler keeps running and non-secret chains still execute.
      Also assert statically that no file under `internal/pgengine/sql/`
      contains `CREATE EXTENSION` or `ALTER EXTENSION`
      (AC-025, AC-027, REQ-007, REQ-053, REQ-054, CON-001).
- [x] T020 [P] `TestSecretGrants` in `internal/pgengine/secrets_test.go`:
      create a throwaway role inside the test and assert it can neither
      `SELECT timetable.secret` nor `EXECUTE` either new function, while the
      owning role can do both. Assert the honest property: the owner **can**
      read `value_enc`, which is why confidentiality rests on the key
      (AC-016, SEC-001).
- [x] T021 [P] `TestResolveSecretsShortCircuit` in
      `internal/pgengine/secrets_test.go` using `pgxmock` via
      `pgengine.NewDB` (the pattern in `internal/pgengine/access_test.go`):
      prove zero round-trips through `mockPool.ExpectationsWereMet()`, not
      merely output equality (AC-017).
- [x] T022 [P] `TestResolveSecretsJSONEscaping` in
      `internal/pgengine/secrets_test.go`: a secret containing `"`, `\`, and a
      newline round-trips through the resolver and `json.Unmarshal`
      byte-for-byte (AC-008).
- [x] T023 [P] `TestResolveSecretsConnStringQuoting` in
      `internal/pgengine/secrets_test.go`: a value with a space and a single
      quote is accepted by `pgx.ParseConfig` and yields the original
      plaintext; an already-delimited `password='${secret:pw}'` template does
      not get doubled delimiters (AC-009).
- [x] T024 [P] `TestResolveSecretsErrorClasses` in
      `internal/pgengine/secrets_test.go`: covers missing secret, wrong
      client scope (indistinguishable from missing), key-unset-with-zero-
      queries, and wrong key (AC-010, AC-011, AC-012).
- [x] T025 [P] `TestSecretStartupCheck` in
      `internal/pgengine/secrets_test.go`: error logged when secrets exist
      without a key; `secret_count()` NOT queried when a key is set — assert
      the negative with `pgxmock` (AC-005, AC-006).
- [x] T026 [P] `TestSecretKeyConfigBinding` in
      `internal/config/config_test.go` (package `config`): drive
      `NewConfig` via `os.Args` and via `PGTT_SECRET_KEY`, following the
      `TestConfigFileFlag` / `TestConfig` patterns. It MUST go through
      `NewConfig`, not `NewCmdOptions` — the latter parses with go-flags
      directly and bypasses viper, so it would pass even with the
      `mapstructure` tag missing and would not defend REQ-016 (AC-022).
- [x] T027 [P] Extend `TestMigrations` in
      `internal/pgengine/migration_test.go` to cover `00798` applying over
      every prior migration, and assert the four-way agreement of the
      migration number (AC-002, AC-003).
- [x] T028 [P] `TestPgxLoggerDropsQueryArgs` in `internal/log/log_test.go`
      (package `log_test`): a marked context drops `args` while retaining
      `sql`; an unmarked context retains both (REQ-030).

**Checkpoint**: Schema, config, redaction plumbing, and resolver exist and are
tested. `go test ./...` is green. Secret values cannot yet reach any task —
that is what the stories add.

---

## Phase 3: User Story 1 - `SendMail` resolves a stored secret (Priority: P1) 🎯 MVP

**Goal**: The one confirmed real-world case works end to end: an SMTP password
lives encrypted in `timetable.secret`, the parameter carries only
`"${secret:smtp_main}"`, and neither `timetable.execution_log.params` nor any
log line ever contains the plaintext.

**Independent Test**: insert a secret for the running client, point a
`SendMail` task's `"password"` field at it, run the chain, and assert
`tasks.SendMail` received the plaintext while `execution_log.params` and
`timetable.log` contain only the reference form.

### Tests for User Story 1

> Write these first and confirm they fail before implementing T031–T033.

- [x] T029 [P] [US1] `TestSendMailResolvesSecret` in
      `internal/scheduler/tasks_test.go` (package `scheduler`): `taskSendMail`
      against a container, with a stub SMTP listener or an injected
      `tasks.SendMail` boundary, asserting the plaintext password reaches
      `EmailConn.Password` (AC-007).
- [x] T030 [P] [US1] `TestBuiltinDebugLogOmitsParamValues` in
      `internal/scheduler/tasks_test.go`: capture logrus output from
      `executeBuiltinTask` and assert a parameter count is present and no
      parameter value is (AC-015).

### Implementation for User Story 1

- [x] T031 [US1] Change `taskSendMail`'s receiver in
      `internal/scheduler/tasks.go` from `_ *Scheduler` to `sch *Scheduler` so
      `sch.pgengine.ResolveSecretsJSON` is reachable. The `BuiltinTasks` map
      type is unchanged (REQ-034).
- [x] T032 [US1] Call `sch.pgengine.ResolveSecretsJSON(ctx, paramValues)` in
      `taskSendMail` before `json.Unmarshal` into `tasks.EmailConn`, returning
      the error unchanged on failure (REQ-036, REQ-043).
- [x] T033 [US1] Confirm `executeBuiltinTask` in
      `internal/scheduler/tasks.go` does NOT resolve secrets and does NOT
      rebind its loop variable `val`. The same `val` is passed to
      `f(ctx, sch, val)` and then to `LogTaskExecution` on the following line;
      rebinding it would write plaintext into `execution_log.params` — the
      exact defect this feature exists to fix (REQ-031, REQ-033).
- [x] T034 [US1] Verify `internal/tasks/mail.go` is untouched and
      `internal/tasks/mail_test.go` still passes unchanged. `mail.go`
      legitimately operates on an already-resolved `EmailConn` (CON-003).
- [x] T035 [US1] Verify no resolved value reaches `internal/otel` — span
      attributes stay `client.name`, `task.name`, `task.kind`,
      `task.return_code` (REQ-035).

**Checkpoint**: US1 is fully functional. The highest-value confirmed case
(`SendMail`) resolves secrets with no plaintext persistence anywhere.

---

## Phase 4: User Story 2 - SQL and remote-connection secrets (Priority: P2)

**Goal**: `SQL` task parameters and `timetable.task.database_connection`
resolve `${secret:name}`, with the reference form persisted to
`execution_log.params` and query arguments redacted from the tracer.

**Independent Test**: run a remote SQL task whose `database_connection`
contains `password=${secret:remotedb_demo}` and a local SQL task with a
secret-bearing parameter; assert both execute and that
`execution_log.params` plus `timetable.log` are clean at debug level.

### Tests for User Story 2

- [x] T036 [P] [US2] `TestExecutionLogNeverContainsPlaintext` in
      `internal/pgengine/secrets_test.go`, SQL path first: assert
      `execution_log.params` holds the literal `${secret:...}` string, never
      the plaintext (AC-013, partial — PROGRAM path lands in T042).
- [x] T037 [P] [US2] `TestPgxTracerRedactsSecretArgs` in
      `internal/pgengine/secrets_test.go`: run a secret-bearing SQL task with
      `--log-level=debug --log-database-level=debug`, then assert
      `timetable.log` contains neither the plaintext nor the encryption key,
      and that the `Query` entries retain `sql` but carry no `args`
      (AC-014, SEC-004).

### Implementation for User Story 2

- [x] T038 [US2] In `ExecuteSQLCommand` (`internal/pgengine/transaction.go`),
      resolve each loop value into a **separate** variable, `json.Unmarshal`
      the resolved text into `params`, and pass the **unresolved** `val` to
      `LogTaskExecution` (REQ-031, REQ-037).
- [x] T039 [US2] In the same function, pass a `log.WithoutQueryArgs` context
      to `executor.Exec(ctx, task.Command, params...)` whenever resolution
      substituted at least one secret, so bound arguments are not written to
      `timetable.log` (REQ-030, REQ-037).
- [x] T040 [US2] In `ExecRemoteSQLTask` (`internal/pgengine/transaction.go`),
      resolve `task.ConnectString` with `ResolveSecretsConnString`
      **eagerly**, into a local variable, before constructing the
      `func() (PgxConnIface, error)` closure handed to `ExecStandaloneTask`.
      Resolving inside the closure would defer the error until after
      `SetRole` and `SetCurrentTaskContext` have already run.
      `task.ConnectString` MUST NOT be overwritten (REQ-038, REQ-040).
- [x] T041 [US2] Add nothing to the remote-connection error path.
      `GetRemoteDBConnection` uses `pgx.Connect`, which carries no tracer, and
      pgx already redacts passwords in its own errors —
      `ParseConfigError.Error` applies `redactPW` and `ConnectError.Error`
      prints only user and database (CON-008).

**Checkpoint**: US1 and US2 both work. SQL, remote, and autonomous task paths
resolve secrets and leak nothing.

---

## Phase 5: User Story 3 - PROGRAM task secrets (Priority: P3)

**Goal**: `PROGRAM` task parameters resolve `${secret:name}` into argv, with
the reference form persisted to `execution_log.params`.

**Independent Test**: run a PROGRAM task whose parameter array contains a
secret reference; assert the child process received the plaintext argument and
`execution_log.params` holds the reference.

### Tests for User Story 3

- [x] T042 [P] [US3] Extend `TestExecutionLogNeverContainsPlaintext` to the
      PROGRAM path, completing AC-013's coverage of all three
      `LogTaskExecution` call sites (`transaction.go`, `tasks.go`,
      `shell.go`).

### Implementation for User Story 3

- [x] T043 [US3] In `ExecuteProgramCommand` (`internal/scheduler/shell.go`),
      resolve each loop value with `ResolveSecretsJSON` into a separate
      variable, `json.Unmarshal` the resolved text into `params` for argv, and
      pass the **unresolved** `val` to `LogTaskExecution`. This is the third
      `LogTaskExecution` call site and is the one most likely to be missed
      (REQ-031, REQ-039).
- [x] T044 [US3] Document, do not fix, the argv exposure: resolved values
      become process argv and are visible to `ps`/auditd on the worker host.
      v1 substitutes anyway because passing the literal `${secret:x}` to a
      child process is silently wrong rather than loudly unsupported
      (SEC-003, GUD-001).

**Checkpoint**: All three task kinds resolve secrets. All resolution surfaces
in the spec are implemented.

---

## Phase 6: User Story 4 - Samples, harness, and documentation (Priority: P4)

**Goal**: The shipped samples demonstrate the feature, the test harness keeps
them self-contained, and the documentation states the trust boundary honestly.

**Independent Test**: `TestSamplesScripts` and `TestRun` pass against a fresh
container with no manual setup, and the samples actually use a resolved secret.

### Tests for User Story 4

- [x] T045 [P] [US4] `TestLegacyLiteralParametersUnchanged` in
      `internal/pgengine/secrets_test.go`: a chain whose `parameter.value`
      holds a literal password behaves identically after the migration.
      `${secret:...}` is opt-in syntax, not a format change (AC-024).

### Implementation for User Story 4

- [x] T046 [US4] Update `internal/testutils/testcontainers.go` to set a fixed
      test `SecretEncryptionKey` on the constructed `CmdOptions` (via the
      existing `customizer` seam or directly), and apply the T002 decision so
      the samples resolve under the harness's
      `--clientname=testcontainers_unit_test`. Do NOT make the harness install
      `pgcrypto` on behalf of the samples: each sample installs it itself
      (T047, T048), which is also what a real user's demo run does (REQ-049).
- [x] T047 [US4] Rework `samples/Mail.sql`: open with
      `CREATE EXTENSION IF NOT EXISTS pgcrypto;` — allowed here because a
      sample is a demo the user runs deliberately, and PROHIBITED in product
      DDL — then insert the secret row with an **unqualified**
      `pgp_sym_encrypt`, replacing the current `timetable.pgp_sym_encrypt`
      call, which only worked under the rejected install-into-`timetable`
      design. Keep the `"password"` field as `"${secret:smtp_main}"` and the
      `-- Legacy (deprecated):` comment. Add a header comment stating that
      pg_timetable itself never installs the extension and that the sample
      does so only to be runnable out of the box (REQ-047, REQ-052, AC-023).
- [x] T048 [US4] Rework `samples/RemoteDB.sql` the same way: add the
      `CREATE EXTENSION IF NOT EXISTS pgcrypto;` demo prologue, keep
      `password=${secret:remotedb_demo}`, replace `timetable.pgp_sym_encrypt`
      with the unqualified call, and keep the note that the demo is
      same-cluster while the pattern applies to genuine cross-host
      connections (REQ-048, REQ-052, AC-023).
- [x] T049 [US4] Confirm `TestSamplesScripts`
      (`internal/pgengine/pgengine_test.go`) and `TestRun`
      (`internal/scheduler/scheduler_test.go`) pass unmodified in name against
      a fresh container with no manual setup (AC-023).
- [x] T050 [P] [US4] Update the "Secrets" subsection in `docs/samples.md` and
      `docs/yaml-usage-guide.md`: `${secret:name}`, the write-only model, the
      manual `GRANT` step for a separate admin role, the PROGRAM argv caveat,
      the debug-level caveat, and the trust boundary. Re-opened because the
      current text presents `pgcrypto` as something the migration installs
      into `timetable`. It MUST instead state that `pgcrypto` is an **optional
      prerequisite the DBA installs**, that pg_timetable never installs or
      requires it, and that a database without it runs normally with only the
      secret store unavailable — and MUST NOT prescribe an installation
      schema (REQ-007, REQ-052, REQ-054). Keep the guidance: prefer `.pgpass`
      / `.pg_service.conf` on the worker host for remote Postgres passwords —
      the store exists for credentials that have no host-local equivalent,
      such as SMTP (REQ-050, REQ-014, SEC-002, SEC-003, SEC-004, GUD-003).
- [x] T051 [P] [US4] Update the prose in `docs/database_schema.md`: keep the
      write-only model and `resolve_secret` usage, and remove the claim that
      the migration installs `pgcrypto` into `timetable` (and any
      `<pgcrypto_schema>.pgp_sym_decrypt` `search_path` wording that implies a
      create-time-fixed schema). State that `resolve_secret` is `LANGUAGE
      plpgsql`, locates the extension at call time, and raises
      `feature_not_supported` when it is absent. The table and function
      definitions appear automatically because the page embeds `ddl.sql`
      through a pymdownx snippet (REQ-007, REQ-008, REQ-051, REQ-054).
- [x] T052 [P] [US4] State the compliance boundary in the new documentation:
      this raises the bar against other database roles, logical replicas, and
      `pg_dump` without the key, but satisfies no specific regulatory control
      on its own (COM-001).
- [x] T053 [P] [US4] Rewrite the deployment-prerequisites documentation
      honestly: nothing is required to run pg_timetable; `pgcrypto` is needed
      **only** by the secret store and is installed by whoever deploys the
      database. Since PostgreSQL 13 it is a **trusted** extension, installable
      by a non-superuser holding `CREATE` on the database, so managed services
      need no superuser; only PostgreSQL 12 and older do. It also requires a
      build with OpenSSL; on a build without one the secret store is simply
      unavailable and everything else is unaffected (INF-001, INF-002,
      REQ-007, REQ-054).
- [x] T054 [US4] Confirm `samples/yaml/*.yaml` are unchanged (CON-004).

**Checkpoint**: All four stories complete. Samples, harness, and docs ship
together with the code.

---

## Phase 7: Polish & Cross-Cutting Concerns

**Purpose**: Whole-feature verification and cleanup.

- [x] T055 Verify the non-goals held: no change to the scheduler's own
      connection/authentication mechanism (CON-005), no versioning/rotation/
      leasing/KMS (CON-006), no new role created by the schema (REQ-009), no
      key stored in any table (REQ-017), no new `go.mod` entry (PLT-001), and
      no `CREATE EXTENSION`/`ALTER EXTENSION` anywhere under
      `internal/pgengine/sql/` — grep for it (REQ-007, CON-001).
- [x] T056 Run the §10 leak verification end to end: execute a chain that
      consumes a secret through builtin, SQL, remote, and PROGRAM paths at
      `--log-level=debug --log-database-level=debug`, then confirm
      `SELECT params FROM timetable.execution_log` holds only reference forms,
      `SELECT message, message_data FROM timetable.log` contains neither the
      plaintext nor the key, and the stdout/file log contains neither.
- [x] T057 Confirm fresh-install and migration paths converge: compare
      `pg_catalog` introspection of `timetable.secret`, its constraint, its
      trigger, and both functions between a database bootstrapped from
      `ddl.sql` and one upgraded through `00798.sql`, on a database with **no**
      `pgcrypto` installed, so the comparison also proves both paths apply
      without the extension (REQ-007, REQ-045, AC-001, AC-002, AC-025).
- [x] T058 Run the full suite once: `go test ./...` plus `go vet ./...` and
      the CI `golangci-lint` configuration, with no new suppressions. Note
      the CI job's 300 s suite timeout — reuse the existing container helpers
      rather than starting a container per test case.
- [x] T059 Confirm every acceptance criterion AC-001 … AC-027 maps to a named
      test that actually runs. CI enforces no numeric coverage threshold, so
      this mapping is the coverage bar (§6, §10).
- [x] T060 Delete `docs/secret-vault-analysis.md` and
      `docs/secret-store-design-brief.md`. Both are superseded — the
      specification is self-contained, and `spec/spec-design-secret-store.md`
      is now the single source of truth.

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no dependencies — start immediately. T002's decision
  gates T046–T048 only.
- **Foundational (Phase 2)**: depends on Setup — BLOCKS all user stories.
- **User Stories (Phases 3–6)**: all depend on Foundational.
  - US1 (P1) is the MVP and has no dependency on US2/US3/US4.
  - US2 and US3 are independent of each other; both only need Foundational.
  - US4 depends on the resolver existing (Foundational) and is best done last
    because its samples exercise US1 and US2 paths.
- **Polish (Phase 7)**: depends on all stories being complete.

### Critical path inside Foundational

- T005 → T006 → T007 → T008 (schema must exist and be verified before any
  resolver test can run against a live server). T008 now also gates T017's
  class-4 handling, since the `0A000` error it must classify is raised by the
  rewritten `resolve_secret`.
- T010 → T011 (the marker must exist before `PgxLogger.Log` can honor it).
- T013 → T014 → {T015, T016} → T017 → T018 (the shared engine precedes the
  two public resolvers, which precede failure semantics and the startup
  check).
- T009 is independent of the schema work and can land at any time.
- T012 is independent of everything and may ship as its own commit.

### User Story Dependencies

- **US1 (P1)**: after Foundational. No dependency on other stories.
- **US2 (P2)**: after Foundational. Independent of US1; both touch different
  files (`transaction.go` vs `tasks.go`).
- **US3 (P3)**: after Foundational. Independent of US1/US2; touches
  `shell.go`. T042 extends a test file that T036 creates — coordinate or
  sequence those two.
- **US4 (P4)**: after Foundational; verified most meaningfully after US1 and
  US2, since `samples/Mail.sql` exercises US1 and `samples/RemoteDB.sql`
  exercises US2.

### Within Each User Story

- Tests are written first and must FAIL before the implementation lands.
- Schema before resolver; resolver before call sites; call sites before
  samples.
- Masking is never deferred: each resolution task ships with its
  `LogTaskExecution` split in the same change.

### Parallel Opportunities

- T003, T004 in Setup.
- T009, T010 in Foundational (different files, no shared state).
- T019–T028 — all Foundational tests are `[P]`, but T019, T019a, T019b and
  T020–T025 all create or extend `internal/pgengine/secrets_test.go`; either
  assign the whole file to one owner or split into `secrets_test.go` and
  `secrets_schema_test.go`.
- T029, T030 in US1.
- T050–T053 in US4 — documentation tasks touching different files.
- US1, US2, and US3 can proceed in parallel across developers once
  Foundational is complete.

---

## Parallel Example: Foundational tests

```bash
# Independent files — safe to run fully in parallel:
Task: "TestSecretKeyConfigBinding in internal/config/config_test.go"
Task: "TestPgxLoggerDropsQueryArgs in internal/log/log_test.go"
Task: "Extend TestMigrations in internal/pgengine/migration_test.go"

# Same file (internal/pgengine/secrets_test.go) — one owner, or split the file:
Task: "TestSecretSchemaFreshInstall"
Task: "TestResolveSecretLocatesPgcrypto"
Task: "TestSecretsWithoutPgcrypto"
Task: "TestSecretGrants"
Task: "TestResolveSecretsShortCircuit"
Task: "TestResolveSecretsJSONEscaping"
Task: "TestResolveSecretsConnStringQuoting"
Task: "TestResolveSecretsErrorClasses"
Task: "TestSecretStartupCheck"
```

---

## Implementation Strategy

### Ship-alone candidate (before anything else)

T012 — the `executeBuiltinTask` debug-log fix — is a confirmed leak in the
current codebase, independent of the secret store. It can be reviewed and
merged on its own, with T030 as its test.

### MVP First (US1 only)

1. Phase 1: Setup
2. Phase 2: Foundational (CRITICAL — blocks all stories)
3. Phase 3: US1 — `SendMail` resolves a stored secret
4. **STOP and VALIDATE**: insert a secret, run a `SendMail` chain, confirm
   `execution_log.params` and `timetable.log` hold only the reference form
5. Ship — this is the confirmed real-world case

### Incremental Delivery

1. Setup + Foundational → schema, config, resolver, redaction in place
2. US1 → `SendMail` works → validate → ship (MVP)
3. US2 → SQL + remote connection strings → validate → ship
4. US3 → PROGRAM argv → validate → ship
5. US4 → samples, harness, docs → validate → ship
6. Each story adds a resolution surface without changing the ones before it

### Parallel Team Strategy

1. Team completes Setup + Foundational together — the schema (T005–T008) and
   the resolver (T013–T018) are the two natural halves and can be split.
2. Once Foundational is done:
   - Developer A: US1 (`internal/scheduler/tasks.go`)
   - Developer B: US2 (`internal/pgengine/transaction.go`)
   - Developer C: US3 (`internal/scheduler/shell.go`) then US4
3. No two stories edit the same file, so they integrate without conflict.

---

## Notes

- [P] tasks = different files, no dependencies.
- [Story] label maps each task to a user story for traceability.
- Every task cites the spec IDs it satisfies; a task is done when those hold,
  not when the code merely compiles.
- Verify tests fail before implementing.
- Commit after each task or logical group.
- Stop at any checkpoint to validate a story independently.
- Five traps worth re-reading before starting, each already cost a spec
  revision:
  1. The migration alone does not reach fresh installs — `ddl.sql` too
     (REQ-045).
  2. pg_timetable never installs an extension. `CREATE EXTENSION` belongs in
     samples and test fixtures only, never under `internal/pgengine/sql/`;
     inside a migration transaction it would turn a privilege or availability
     problem into a permanent startup failure (REQ-007, REQ-052, CON-001).
  3. `resolve_secret` must be `LANGUAGE plpgsql`, with the pgcrypto lookup and
     the decrypt call in dynamic SQL. A `LANGUAGE sql` body — qualified or
     not — is resolved at `CREATE FUNCTION` time and fails on any database
     without `pgcrypto`, taking the migration and startup down with it
     (REQ-053).
  4. A missing extension is a per-task error (`0A000`), never a startup error
     and never a probe: no code outside `resolve_secret`'s body may look for
     `pgcrypto` (REQ-054).
  5. `mapstructure:"secret-key"` is required or the config field silently
     stays empty, and only a `NewConfig`-based test catches it (REQ-016).
- Avoid: rebinding `val` before `LogTaskExecution`, flat string substitution
  on jsonb, referencing role names the project does not create, adding a
  `SELECT` grant on `timetable.secret`, and writing `timetable.pgp_sym_encrypt`
  anywhere — the extension's schema is the deployer's choice, not ours.
