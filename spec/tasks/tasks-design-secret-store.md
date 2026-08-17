---
description: "Task list for implementing the Postgres-native secret store (timetable.secret)"
---

# Tasks: Postgres-Native Secret Store (`timetable.secret`)

**Input**: `spec/spec-design-secret-store.md` (v2.0)
**Prerequisites**: that spec is self-contained; no other design document is required.

**Tests**: Tests ARE requested. The specification mandates them explicitly — §6
names every test to add, and §10 requires that all 25 acceptance criteria
(AC-001 … AC-025) map to at least one named test. Test tasks below are
therefore REQUIRED, not optional.

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

- [ ] T001 Record the pre-change baseline: run `go test ./...` and save the
      result. Every later phase must leave the suite at least as green. Note
      that `internal/pgengine/pgengine_test.go` (`TestSamplesScripts`) and
      `internal/scheduler/scheduler_test.go` (`TestRun`) currently pass and
      will be affected by Phase 6 (REQ-049).
- [ ] T002 Decide and record the REQ-049 sample self-containment mechanism —
      the spec deliberately leaves this open. Choose one:
      (a) samples derive the client from
      `current_setting('pg_timetable.current_client_name', true)`, or
      (b) samples use a literal placeholder and
      `internal/testutils/testcontainers.go` seeds a matching secret.
      The chosen mechanism MUST make `samples/*.sql` runnable by
      `TestSamplesScripts`, which executes them with no manual setup. Write
      the decision into the header comment of `samples/Mail.sql` so it is
      discoverable at the point of use.
- [ ] T003 [P] Fix the migration number. Confirm the highest registered
      migration in `internal/pgengine/migration.go` is still `00797`; if
      another migration has landed, use the next free number and apply it
      consistently to the migration file name, the `migration.go` entry, the
      `internal/pgengine/sql/init.sql` seed row, and `main.go`'s `dbapi`
      (REQ-046, AC-003). All four MUST agree.
- [ ] T004 [P] Confirm the local verification path for SQL work: either
      Docker (for `testcontainers-go`) or a local PostgreSQL instance. The
      spec's §4.1 SQL was authored against PostgreSQL 16.1 and MUST be
      re-executed in both extension scenarios during Phase 2 (AC-004,
      AC-025).

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Schema, configuration, redaction plumbing, and the resolver. No
story can resolve or mask a secret until this phase is complete.

**⚠️ CRITICAL**: No user story work can begin until this phase is complete.

### Schema and migration

- [ ] T005 Write the schema block into `internal/pgengine/sql/ddl.sql`,
      appended after the existing tables: `pgcrypto` acquisition, the
      `timetable.secret` table with `PRIMARY KEY (client_name, secret_name)`
      and no surrogate id, the `secret_name_format` CHECK, all five
      `COMMENT`s, `REVOKE ALL ... FROM PUBLIC`, `timetable.secret_touch()` +
      the `secret_touch` `BEFORE UPDATE` trigger, `timetable.resolve_secret`,
      and `timetable.secret_count`. Copy §4.1 of the spec verbatim — it was
      executed against a live server and is known to apply cleanly
      (REQ-001, REQ-002, REQ-003, REQ-004, REQ-005, REQ-006, REQ-007,
      REQ-010, REQ-011, REQ-012, REQ-013, REQ-014, SEC-005, SEC-006,
      PAT-001, PLT-002, CON-001, CON-007, DAT-001).
      Critical details that are easy to get wrong:
      - `resolve_secret` MUST be generated through `EXECUTE format(...)` with
        `%I` interpolating the schema `pgcrypto` actually occupies. A plain
        `CREATE FUNCTION` with an unqualified `pgp_sym_decrypt` plus a later
        `ALTER FUNCTION ... SET search_path` FAILS at creation time on any
        database where `pgcrypto` is not in `timetable` (REQ-008, REQ-053).
      - Use `EXECUTE PROCEDURE` and plain `CREATE TRIGGER`, not
        `EXECUTE FUNCTION` / `CREATE OR REPLACE TRIGGER` (PLT-002).
      - Reference no role name (REQ-009). A `GRANT` to a nonexistent role
        aborts the whole migration transaction and blocks startup.
- [ ] T006 Create `internal/pgengine/sql/migrations/00798.sql` containing the
      identical object definitions from T005. The migration alone is
      insufficient and the DDL alone is insufficient — `ExecuteSchemaScripts`
      runs `ddl.sql` only when the `timetable` schema is absent, while
      `init.sql` seeds `timetable.migration` through the current release, so a
      fresh database never runs new migrations (REQ-045, PAT-002).
- [ ] T007 Register the migration in all three places, per the in-code comment
      in `internal/pgengine/migration.go`: the appended
      `&migrator.Migration{Name: "00798 Add timetable.secret store", ...}`
      entry, the `(18, '00798 Add timetable.secret store')` row in
      `internal/pgengine/sql/init.sql`, and `dbapi = "00798"` in `main.go`
      (REQ-046, AC-003).
- [ ] T008 Verify the schema against a live server in BOTH extension
      scenarios before proceeding: (a) `pgcrypto` absent → installed into
      `timetable`; (b) `pgcrypto` pre-existing in `public` → `pg_proc.prosrc`
      contains `public.pgp_sym_decrypt` and decryption succeeds (AC-001,
      AC-004). Also confirm a missing secret yields one row containing NULL
      (not zero rows) and a wrong key raises `Wrong key or corrupt data`.

### Configuration

- [ ] T009 [P] Add `SecretEncryptionKey` to `CmdOptions` in
      `internal/config/cmdparser.go` with all three tags:
      `long:"secret-key" mapstructure:"secret-key" env:"PGTT_SECRET_KEY"`.
      The `mapstructure` tag is load-bearing, not cosmetic: `NewConfig` binds
      flags into viper by long name and then calls `v.Unmarshal`, which
      matches `secret-key` to the field only through that tag. Copy the
      `NoProgramTasks` field as the pattern — NOT `ClientName`/`ConnStr`,
      which work only incidentally because their flag names already match
      their field names (REQ-015, REQ-016, PAT-003).

### Log redaction plumbing

- [ ] T010 [P] Add `WithoutQueryArgs(ctx context.Context) context.Context` and
      a private `noQueryArgs(ctx)` predicate to `internal/log/log.go`, using
      an unexported context-key type consistent with the existing
      `loggerKey struct{}` (REQ-030).
- [ ] T011 In `PgxLogger.Log` (`internal/log/log.go`), delete the `args` key
      from `data` when the context is marked, before the fields are attached
      to the logger. Retain `sql` — command text is logged verbatim by design
      (REQ-023, REQ-030). This is the fix for the most severe leak: at
      `--log-level=debug` the pgx tracer logs every query's bound arguments,
      `tracelog.logQueryArgs` truncates but does not redact, and those entries
      are persisted into the `timetable.log` table by `LogHook.send`.
- [ ] T012 Fix the confirmed standalone defect in
      `internal/scheduler/tasks.go` (`executeBuiltinTask`): replace
      `Debugf("Executing builtin task with parameters %+q", paramValues)` with
      a parameter **count** only (REQ-032). This task is independently
      shippable — it is a real leak today, before any secret store exists, and
      may be merged on its own.

### Resolver

- [ ] T013 Create `internal/pgengine/secrets.go` with `secretRefPattern =
      regexp.MustCompile(`\$\{secret:([A-Za-z0-9_.-]+)\}`)` and the shared
      `resolveRefs` engine: fixed client scope `pge.ClientName`, one
      `timetable.resolve_secret` call per match through `pge.ConfigDb`, no
      recursion into resolved values, and the query context wrapped in
      `log.WithoutQueryArgs` so the encryption key never reaches the tracer
      (REQ-018, REQ-021, REQ-022, REQ-024, REQ-025, REQ-030, REQ-040,
      GUD-002).
- [ ] T014 Implement the mandatory short-circuit in `resolveRefs`: if the
      input lacks the literal substring `${secret:`, return it byte-identical
      with no JSON parsing, no regexp evaluation, and no database round-trip.
      This is a correctness guarantee, not an optimization — it preserves
      existing behavior (including existing malformed-JSON error paths) for
      every parameter that uses no secrets (REQ-026, CON-002, AC-017).
- [ ] T015 Implement `ResolveSecretsJSON` in
      `internal/pgengine/secrets.go`: decode the parameter, substitute inside
      **string leaves only**, and re-encode with `encoding/json` so escaping
      is handled. Flat substitution on raw jsonb text is PROHIBITED — a
      password containing `"`, `\`, or a newline would corrupt the document
      and break the downstream `json.Unmarshal` (REQ-027, REQ-029, AC-008).
- [ ] T016 Implement `ResolveSecretsConnString` in
      `internal/pgengine/secrets.go` with libpq conninfo quoting: wrap in
      single quotes and backslash-escape `\` and `'` when the value is empty
      or contains whitespace, `'`, or `\`; omit the wrapping when the
      reference is already delimited by single quotes in the template so the
      delimiters are not doubled. An empty value MUST emit `''`, because a
      bare `password=` would swallow the next token (REQ-028, REQ-029,
      AC-009).
- [ ] T017 Implement the three distinguished failure classes in
      `internal/pgengine/secrets.go` (REQ-041, REQ-042, REQ-043, REQ-044):
      1. **Missing secret** — scan into a nullable target (`*string` or
         `pgtype.Text`) and treat NULL as not found. Do NOT rely on
         `pgx.ErrNoRows`; a `LANGUAGE sql` scalar function returns one row
         containing NULL, so `ErrNoRows` never occurs on this path. Error text
         must name the secret and the client scope, and must be identical for
         "exists under another client" so existence does not leak.
      2. **Key unset** — fail before issuing any query when a reference is
         present and `SecretEncryptionKey` is empty
         (`pgp_sym_encrypt(x, '')` is legal, so an empty key otherwise yields
         a confusing corrupt-data error).
      3. **Wrong key** — wrap the `Wrong key or corrupt data` error with the
         secret name; never report it as not-found.
      Silent empty-string substitution is PROHIBITED in all three.
- [ ] T018 Implement `CheckSecretConfig` in `internal/pgengine/secrets.go` and
      call it from `run()` in `main.go`, positioned after the
      migration/upgrade block (so the schema is known current) and before
      `scheduler.New`. It MUST return immediately without querying when
      `SecretEncryptionKey` is non-empty, and otherwise call
      `timetable.secret_count()` exactly once and log an error when the count
      exceeds zero. A failure of the check itself is logged, never fatal
      (REQ-013, REQ-019, REQ-020, CON-002).

### Foundational tests

- [ ] T019 [P] `TestSecretSchemaFreshInstall` in a new
      `internal/pgengine/secrets_test.go` (package `pgengine_test`, using
      `testutils.SetupPostgresContainer`): asserts table/functions/trigger
      exist, the pre-existing-`pgcrypto` path works, the `secret_touch`
      trigger overrides a falsified `updated_at='epoch', updated_by='liar'`
      UPDATE, `secret_name_format` rejects `'has space'` and `''`, NULL
      `client_name` is rejected, and per-client isolation holds
      (AC-001, AC-004, AC-018, AC-019, AC-020, AC-021).
- [ ] T020 [P] `TestSecretGrants` in `internal/pgengine/secrets_test.go`:
      create a throwaway role inside the test and assert it can neither
      `SELECT timetable.secret` nor `EXECUTE` either new function, while the
      owning role can do both. Assert the honest property: the owner **can**
      read `value_enc`, which is why confidentiality rests on the key
      (AC-016, SEC-001).
- [ ] T021 [P] `TestResolveSecretsShortCircuit` in
      `internal/pgengine/secrets_test.go` using `pgxmock` via
      `pgengine.NewDB` (the pattern in `internal/pgengine/access_test.go`):
      prove zero round-trips through `mockPool.ExpectationsWereMet()`, not
      merely output equality (AC-017).
- [ ] T022 [P] `TestResolveSecretsJSONEscaping` in
      `internal/pgengine/secrets_test.go`: a secret containing `"`, `\`, and a
      newline round-trips through the resolver and `json.Unmarshal`
      byte-for-byte (AC-008).
- [ ] T023 [P] `TestResolveSecretsConnStringQuoting` in
      `internal/pgengine/secrets_test.go`: a value with a space and a single
      quote is accepted by `pgx.ParseConfig` and yields the original
      plaintext; an already-delimited `password='${secret:pw}'` template does
      not get doubled delimiters (AC-009).
- [ ] T024 [P] `TestResolveSecretsErrorClasses` in
      `internal/pgengine/secrets_test.go`: covers missing secret, wrong
      client scope (indistinguishable from missing), key-unset-with-zero-
      queries, and wrong key (AC-010, AC-011, AC-012).
- [ ] T025 [P] `TestSecretStartupCheck` in
      `internal/pgengine/secrets_test.go`: error logged when secrets exist
      without a key; `secret_count()` NOT queried when a key is set — assert
      the negative with `pgxmock` (AC-005, AC-006).
- [ ] T026 [P] `TestSecretKeyConfigBinding` in
      `internal/config/config_test.go` (package `config`): drive
      `NewConfig` via `os.Args` and via `PGTT_SECRET_KEY`, following the
      `TestConfigFileFlag` / `TestConfig` patterns. It MUST go through
      `NewConfig`, not `NewCmdOptions` — the latter parses with go-flags
      directly and bypasses viper, so it would pass even with the
      `mapstructure` tag missing and would not defend REQ-016 (AC-022).
- [ ] T027 [P] Extend `TestMigrations` in
      `internal/pgengine/migration_test.go` to cover `00798` applying over
      every prior migration, and assert the four-way agreement of the
      migration number (AC-002, AC-003).
- [ ] T028 [P] `TestPgxLoggerDropsQueryArgs` in `internal/log/log_test.go`
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

- [ ] T029 [P] [US1] `TestSendMailResolvesSecret` in
      `internal/scheduler/tasks_test.go` (package `scheduler`): `taskSendMail`
      against a container, with a stub SMTP listener or an injected
      `tasks.SendMail` boundary, asserting the plaintext password reaches
      `EmailConn.Password` (AC-007).
- [ ] T030 [P] [US1] `TestBuiltinDebugLogOmitsParamValues` in
      `internal/scheduler/tasks_test.go`: capture logrus output from
      `executeBuiltinTask` and assert a parameter count is present and no
      parameter value is (AC-015).

### Implementation for User Story 1

- [ ] T031 [US1] Change `taskSendMail`'s receiver in
      `internal/scheduler/tasks.go` from `_ *Scheduler` to `sch *Scheduler` so
      `sch.pgengine.ResolveSecretsJSON` is reachable. The `BuiltinTasks` map
      type is unchanged (REQ-034).
- [ ] T032 [US1] Call `sch.pgengine.ResolveSecretsJSON(ctx, paramValues)` in
      `taskSendMail` before `json.Unmarshal` into `tasks.EmailConn`, returning
      the error unchanged on failure (REQ-036, REQ-043).
- [ ] T033 [US1] Confirm `executeBuiltinTask` in
      `internal/scheduler/tasks.go` does NOT resolve secrets and does NOT
      rebind its loop variable `val`. The same `val` is passed to
      `f(ctx, sch, val)` and then to `LogTaskExecution` on the following line;
      rebinding it would write plaintext into `execution_log.params` — the
      exact defect this feature exists to fix (REQ-031, REQ-033).
- [ ] T034 [US1] Verify `internal/tasks/mail.go` is untouched and
      `internal/tasks/mail_test.go` still passes unchanged. `mail.go`
      legitimately operates on an already-resolved `EmailConn` (CON-003).
- [ ] T035 [US1] Verify no resolved value reaches `internal/otel` — span
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

- [ ] T036 [P] [US2] `TestExecutionLogNeverContainsPlaintext` in
      `internal/pgengine/secrets_test.go`, SQL path first: assert
      `execution_log.params` holds the literal `${secret:...}` string, never
      the plaintext (AC-013, partial — PROGRAM path lands in T042).
- [ ] T037 [P] [US2] `TestPgxTracerRedactsSecretArgs` in
      `internal/pgengine/secrets_test.go`: run a secret-bearing SQL task with
      `--log-level=debug --log-database-level=debug`, then assert
      `timetable.log` contains neither the plaintext nor the encryption key,
      and that the `Query` entries retain `sql` but carry no `args`
      (AC-014, SEC-004).

### Implementation for User Story 2

- [ ] T038 [US2] In `ExecuteSQLCommand` (`internal/pgengine/transaction.go`),
      resolve each loop value into a **separate** variable, `json.Unmarshal`
      the resolved text into `params`, and pass the **unresolved** `val` to
      `LogTaskExecution` (REQ-031, REQ-037).
- [ ] T039 [US2] In the same function, pass a `log.WithoutQueryArgs` context
      to `executor.Exec(ctx, task.Command, params...)` whenever resolution
      substituted at least one secret, so bound arguments are not written to
      `timetable.log` (REQ-030, REQ-037).
- [ ] T040 [US2] In `ExecRemoteSQLTask` (`internal/pgengine/transaction.go`),
      resolve `task.ConnectString` with `ResolveSecretsConnString`
      **eagerly**, into a local variable, before constructing the
      `func() (PgxConnIface, error)` closure handed to `ExecStandaloneTask`.
      Resolving inside the closure would defer the error until after
      `SetRole` and `SetCurrentTaskContext` have already run.
      `task.ConnectString` MUST NOT be overwritten (REQ-038, REQ-040).
- [ ] T041 [US2] Add nothing to the remote-connection error path.
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

- [ ] T042 [P] [US3] Extend `TestExecutionLogNeverContainsPlaintext` to the
      PROGRAM path, completing AC-013's coverage of all three
      `LogTaskExecution` call sites (`transaction.go`, `tasks.go`,
      `shell.go`).

### Implementation for User Story 3

- [ ] T043 [US3] In `ExecuteProgramCommand` (`internal/scheduler/shell.go`),
      resolve each loop value with `ResolveSecretsJSON` into a separate
      variable, `json.Unmarshal` the resolved text into `params` for argv, and
      pass the **unresolved** `val` to `LogTaskExecution`. This is the third
      `LogTaskExecution` call site and is the one most likely to be missed
      (REQ-031, REQ-039).
- [ ] T044 [US3] Document, do not fix, the argv exposure: resolved values
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

- [ ] T045 [P] [US4] `TestLegacyLiteralParametersUnchanged` in
      `internal/pgengine/secrets_test.go`: a chain whose `parameter.value`
      holds a literal password behaves identically after the migration.
      `${secret:...}` is opt-in syntax, not a format change (AC-024).

### Implementation for User Story 4

- [ ] T046 [US4] Update `internal/testutils/testcontainers.go` to set a fixed
      test `SecretEncryptionKey` on the constructed `CmdOptions` (via the
      existing `customizer` seam or directly), and apply the T002 decision so
      the samples resolve under the harness's
      `--clientname=testcontainers_unit_test` (REQ-049).
- [ ] T047 [US4] Update `samples/Mail.sql`: insert a secret row using
      **`timetable.pgp_sym_encrypt`** — schema-qualified, because samples run
      in a plain session via `ExecuteCustomScripts` and an unqualified call
      fails with `function pgp_sym_encrypt(unknown, unknown) does not exist`
      when `pgcrypto` lives in `timetable`. Change the `"password"` field to
      `"${secret:smtp_main}"` and retain a `-- Legacy (deprecated):` comment
      showing the prior literal (REQ-047, REQ-052, AC-023, AC-025).
- [ ] T048 [US4] Update `samples/RemoteDB.sql`: replace `password=somestrong`
      with `password=${secret:remotedb_demo}`, insert the secret with
      `timetable.pgp_sym_encrypt`, and note that the demo is same-cluster
      while the pattern applies to genuine cross-host connections
      (REQ-048, REQ-052, AC-023).
- [ ] T049 [US4] Confirm `TestSamplesScripts`
      (`internal/pgengine/pgengine_test.go`) and `TestRun`
      (`internal/scheduler/scheduler_test.go`) pass unmodified in name against
      a fresh container with no manual setup (AC-023, AC-025).
- [ ] T050 [P] [US4] Add a "Secrets" subsection to `docs/samples.md` and
      `docs/yaml-usage-guide.md` covering `${secret:name}`, the write-only
      model, the manual `GRANT` step for a separate admin role, the PROGRAM
      argv caveat, the debug-level caveat, and the trust boundary. State the
      guidance too: prefer `.pgpass` / `.pg_service.conf` on the worker host
      for remote Postgres passwords — the store exists for credentials that
      have no host-local equivalent, such as SMTP
      (REQ-050, REQ-014, SEC-002, SEC-003, SEC-004, GUD-003).
- [ ] T051 [P] [US4] Add prose to `docs/database_schema.md` covering the
      write-only model and `resolve_secret` usage. The table and function
      definitions appear automatically because the page embeds `ddl.sql`
      through a pymdownx snippet (REQ-051).
- [ ] T052 [P] [US4] State the compliance boundary in the new documentation:
      this raises the bar against other database roles, logical replicas, and
      `pg_dump` without the key, but satisfies no specific regulatory control
      on its own (COM-001).
- [ ] T053 [P] [US4] Document the deployment prerequisites: `pgcrypto` is a
      **trusted** extension since PostgreSQL 13, installable by a non-superuser
      holding `CREATE` on the database — so managed services need no
      superuser; only PostgreSQL 12 and older do. It also requires a build
      with OpenSSL (INF-001, INF-002).
- [ ] T054 [US4] Confirm `samples/yaml/*.yaml` are unchanged (CON-004).

**Checkpoint**: All four stories complete. Samples, harness, and docs ship
together with the code.

---

## Phase 7: Polish & Cross-Cutting Concerns

**Purpose**: Whole-feature verification and cleanup.

- [ ] T055 Verify the non-goals held: no change to the scheduler's own
      connection/authentication mechanism (CON-005), no versioning/rotation/
      leasing/KMS (CON-006), no new role created by the schema (REQ-009), no
      key stored in any table (REQ-017), and no new `go.mod` entry (PLT-001).
- [ ] T056 Run the §10 leak verification end to end: execute a chain that
      consumes a secret through builtin, SQL, remote, and PROGRAM paths at
      `--log-level=debug --log-database-level=debug`, then confirm
      `SELECT params FROM timetable.execution_log` holds only reference forms,
      `SELECT message, message_data FROM timetable.log` contains neither the
      plaintext nor the key, and the stdout/file log contains neither.
- [ ] T057 Confirm fresh-install and migration paths converge: compare
      `pg_catalog` introspection of `timetable.secret`, its constraint, its
      trigger, and both functions between a database bootstrapped from
      `ddl.sql` and one upgraded through `00798.sql` (REQ-045, AC-001,
      AC-002).
- [ ] T058 Run the full suite once: `go test ./...` plus `go vet ./...` and
      the CI `golangci-lint` configuration, with no new suppressions. Note
      the CI job's 300 s suite timeout — reuse the existing container helpers
      rather than starting a container per test case.
- [ ] T059 Confirm every acceptance criterion AC-001 … AC-025 maps to a named
      test that actually runs. CI enforces no numeric coverage threshold, so
      this mapping is the coverage bar (§6, §10).
- [ ] T060 Delete `docs/secret-vault-analysis.md` and
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
  resolver test can run against a live server).
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
- T019–T028 — all Foundational tests are `[P]`, but T019–T025 all create or
  extend `internal/pgengine/secrets_test.go`; either assign the whole file to
  one owner or split into `secrets_test.go` and
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
- Four traps worth re-reading before starting, each already cost a spec
  revision:
  1. The migration alone does not reach fresh installs — `ddl.sql` too
     (REQ-045).
  2. `resolve_secret` must be generated with the pgcrypto schema baked into
     its body; a post-hoc `ALTER FUNCTION` fails at creation time (REQ-053).
  3. Write-side `pgp_sym_encrypt` calls in samples must be schema-qualified
     (REQ-052).
  4. `mapstructure:"secret-key"` is required or the config field silently
     stays empty, and only a `NewConfig`-based test catches it (REQ-016).
- Avoid: rebinding `val` before `LogTaskExecution`, flat string substitution
  on jsonb, referencing role names the project does not create, and adding a
  `SELECT` grant on `timetable.secret`.
