---
title: pgtt — Command-Line Management Tool for pg_timetable
version: 0.1 (draft)
date_created: 2026-06-23
last_updated: 2026-06-23
owner: pg_timetable maintainers
tags: [tool, cli, architecture, postgresql, scheduler]
---

# Introduction

`pgtt` is a command-line management tool for **pg_timetable**, a PostgreSQL-driven
job scheduler. `pgtt` lets operators inspect, create, edit, control, and observe
scheduler chains and tasks without manually crafting SQL over an SSH session. This
specification defines the requirements, constraints, interfaces, and acceptance
criteria for version 1 (v1) of `pgtt`.

## 1. Purpose & Scope

### Purpose

Provide a first-class operational interface for managing pg_timetable so that
administrators can manage tens to hundreds of chains efficiently, scriptably, and
(in a later phase) interactively.

### In Scope (v1)

- Read/inspect chains, tasks, parameters, active sessions, active chains, and logs.
- Full create/read/update/delete (CRUD) of chains, tasks, and parameters.
- Live control: trigger ("start now") and cancel ("stop now") chains; pause/resume.
- Import/apply chains from YAML and export chains to YAML.
- Fleet awareness: operate across multiple scheduler instances sharing one database,
  distinguished by `client_name`.

### Out of Scope (v1)

- Interactive k9s-style terminal user interface (TUI). Deferred to a later phase but
  the internal architecture MUST NOT preclude it.
- New REST API endpoints in the pg_timetable core (e.g. reload, restart, list-active).
- Managing the lifecycle of the scheduler OS process itself (start/stop the daemon).
- Authentication/authorization beyond what the PostgreSQL connection already enforces.

### Intended Audience

Database administrators and DevOps engineers operating pg_timetable deployments;
contributors implementing `pgtt`.

### Assumptions

- The target database already contains the `timetable.*` schema created by a running
  or previously-run pg_timetable instance.
- The operator possesses PostgreSQL credentials with sufficient privileges on the
  `timetable` schema.

## 2. Definitions

| Term | Definition |
|------|------------|
| **Chain** | A scheduled unit in pg_timetable; row in `timetable.chain`. Has a schedule (`run_at`), `live` flag, and an owning preference via `client_name`. |
| **Task** | An ordered step within a chain; row in `timetable.task`. Has a `kind` (SQL, PROGRAM, BUILTIN) and a `command`. |
| **Parameter** | A JSONB argument bound to a task; row in `timetable.parameter`. |
| **client_name** | Identifier of a scheduler instance (worker). Also the PostgreSQL `LISTEN/NOTIFY` channel name used to signal that worker. |
| **Scheduler instance / worker** | A running pg_timetable process connected to the database, identified by its `client_name`. |
| **Fleet** | The set of scheduler instances sharing one database. |
| **Live control** | Triggering or cancelling a chain run immediately, as opposed to relying on its schedule. |
| **TUI** | Text/terminal user interface (e.g. k9s-style). |
| **DSN / connstring** | PostgreSQL connection string. |
| **REQ/SEC/CON/GUD/PAT/AC** | Requirement / Security requirement / Constraint / Guideline / Pattern / Acceptance Criterion identifiers. |

## 3. Requirements, Constraints & Guidelines

### Functional Requirements

- **REQ-001**: `pgtt` SHALL connect to PostgreSQL directly and treat the `timetable.*`
  schema as the single source of truth.
- **REQ-002**: `pgtt` SHALL list chains with at minimum: `chain_id`, `chain_name`,
  `run_at`, `live`, `max_instances`, `client_name`, last execution status (derived
  from `timetable.execution_log`), and currently-active state (derived from
  `timetable.active_chain`).
- **REQ-003**: `pgtt` SHALL show the tasks of a chain, including `task_order`, `kind`,
  `command`, `database_connection`, `ignore_error`, `autonomous`, and `timeout`.
- **REQ-004**: `pgtt` SHALL create, update, and delete chains, tasks, and parameters.
- **REQ-005**: `pgtt` SHALL trigger a single, immediate (one-shot) run of a chain by
  invoking `timetable.notify_chain_start(chain_id, worker_name, delay)`. This manual
  trigger SHALL execute the chain exactly once regardless of the chain's `live` flag
  (intended for debugging/ad-hoc runs) and SHALL NOT alter the chain's schedule or
  `live` state. `--worker` (the target `client_name`) is REQUIRED.
- **REQ-006**: `pgtt` SHALL cancel a running chain by invoking
  `timetable.notify_chain_stop(chain_id, worker_name)`. `--worker` (the target
  `client_name`) is REQUIRED.
- **REQ-007**: `pgtt` SHALL pause and resume chains via `timetable.pause_job(name)`
  and `timetable.resume_job(name)` (equivalently by toggling `chain.live`).
- **REQ-008**: `pgtt` SHALL reorder tasks within a chain via
  `timetable.move_task_up(task_id)` and `timetable.move_task_down(task_id)`.
- **REQ-009**: `pgtt` SHALL import/apply chains from a YAML file, reusing the existing
  `pgengine.LoadYamlChains` codec, and SHALL support a replace mode equivalent to the
  existing `--replace` behavior.
- **REQ-010**: `pgtt` SHALL export one or more chains to YAML compatible with the
  import format (`pgengine.YamlConfig`). (Export is net-new; no core function exists yet.)
- **REQ-011**: `pgtt` SHALL list active sessions (`timetable.active_session`) and
  active chains (`timetable.active_chain`) to provide fleet visibility.
- **REQ-012**: `pgtt` SHALL display log entries from `timetable.log` and
  `timetable.execution_log`, filterable by chain and by `client_name`.
- **REQ-013**: `pgtt` SHALL support a follow/tail mode for logs using PostgreSQL
  `LISTEN/NOTIFY`, consistent with the mechanism in `internal/pgengine/notification.go`.
- **REQ-014**: Every mutating command SHALL be expressible non-interactively (flags
  only) so it can run over SSH and in CI without a TTY.
- **REQ-015**: `pgtt` SHALL provide machine-readable output (JSON) for list/show
  commands via a `--output json` (or `-o json`) flag, defaulting to a human-readable table.
- **REQ-016**: `pgtt` SHALL verify the database schema version on connect and refuse
  to operate (with a clear message) against an incompatible schema version.

### Security Requirements

- **SEC-001**: `pgtt` SHALL NOT store PostgreSQL passwords in plaintext config that it
  creates by default; it SHALL support standard libpq environment variables and
  connection strings supplied by the user.
- **SEC-002**: `pgtt` SHALL NOT print credentials (passwords, full DSNs containing
  passwords) in logs, error messages, or `--output json`.
- **SEC-003**: Destructive operations (delete chain/task, replace-on-apply) SHALL
  require an explicit confirmation flag (e.g. `--yes`) or interactive confirmation when
  attached to a TTY.

### Constraints

- **CON-001**: `pgtt` SHALL reside in the pg_timetable repository as a separate build
  target at `cmd/pgtt/`.
- **CON-002**: `pgtt` SHALL reuse the existing internal packages `internal/pgengine`
  (connection bootstrap, embedded SQL, domain types, YAML codec), `internal/config`,
  and `internal/log`, rather than duplicating them.
- **CON-003**: v1 transport SHALL be PostgreSQL only. No dependency on the
  pg_timetable REST API is permitted in v1.
- **CON-004**: `pgtt` SHALL use `cobra` for command structure and `viper` for
  configuration/precedence, consistent with the dependency already present in the repo.
- **CON-005**: `pgtt` SHALL be a single statically-linkable Go binary with no runtime
  dependency on the pg_timetable scheduler process.
- **CON-006**: `pgtt` MUST NOT require any new endpoints or changes to the core
  scheduler runtime to deliver v1 functionality.

### Guidelines

- **GUD-001**: Prefer existing `timetable.*` SQL functions over ad-hoc DML so that
  `pgtt` behavior stays identical to the scheduler's own semantics.
- **GUD-002**: Keep all data access behind an internal client layer so the future TUI
  consumes the same API as the scriptable commands.
- **GUD-003**: Default to safe, read-only behavior; require explicit flags for mutations.
- **GUD-004**: Command naming SHOULD follow a `noun verb` or `noun subnoun verb`
  hierarchy (e.g. `pgtt chain list`, `pgtt chain task add`).
- **GUD-005**: Output formatting SHOULD be stable and parseable; avoid breaking JSON
  field names across minor versions.

### Patterns

- **PAT-001**: Source-of-truth pattern — Postgres holds all state; `pgtt` is a stateless
  client.
- **PAT-002**: Thin-wrapper pattern — live start/stop are wrappers over the same
  `pg_notify` functions the REST API uses, so SQL-only transport loses no capability.
- **PAT-003**: Shared-core pattern — scriptable commands and the future TUI share one
  internal client package.

## 4. Interfaces & Data Contracts

### 4.1 Command surface (v1)

```text
pgtt [global flags] <command> [subcommand] [flags] [args]

Global flags:
  --dsn / connstring (positional)   PostgreSQL connection string
  -o, --output {table|json}         Output format (default: table)
  --yes                             Skip confirmation prompts
  --config <path>                   pgtt config file (viper)
  -v, --verbose                     Verbose logging

chain
  list                              List chains (REQ-002)
  show <chain-id|name>              Show chain + its tasks (REQ-003)
  create [flags]                    Create a chain (REQ-004)
  edit <chain-id|name> [flags]      Update chain attributes (REQ-004)
  delete <chain-id|name>            Delete chain (REQ-004, SEC-003)
  start <chain-id> --worker NAME [--delay D]     Run once now (REQ-005)
  stop  <chain-id> --worker NAME    Cancel running chain (REQ-006)
  pause <chain-id|name>             Set live=false (REQ-007)
  resume <chain-id|name>            Set live=true (REQ-007)

chain task
  add <chain-id> [flags]            Add task (REQ-004)
  edit <task-id> [flags]            Update task (REQ-004)
  delete <task-id>                  Delete task (REQ-004, SEC-003)
  move <task-id> {up|down}          Reorder task (REQ-008)

apply  <file.yaml> [--replace]      Import chains from YAML (REQ-009)
export <chain-id|name>... [-f out]  Export chains to YAML (REQ-010)

session list                        Active sessions (REQ-011)
active  list                        Active chains (REQ-011)

log
  list   [--chain ID] [--client NAME] [--limit N]   Query logs (REQ-012)
  tail   [--chain ID] [--client NAME]               Follow logs (REQ-013)
```

### 4.2 SQL control-plane contract (reused, MUST NOT be re-implemented)

| Operation | SQL function | Notes |
|-----------|--------------|-------|
| Start now | `timetable.notify_chain_start(chain_id BIGINT, worker_name TEXT, start_delay INTERVAL DEFAULT NULL)` | Emits `pg_notify(worker_name, {ConfigID, Command:"START", Ts, Delay})` |
| Stop now | `timetable.notify_chain_stop(chain_id BIGINT, worker_name TEXT)` | Emits `pg_notify(worker_name, {ConfigID, Command:"STOP", Ts})` |
| Pause | `timetable.pause_job(job_name TEXT) RETURNS boolean` | toggles `chain.live=false` |
| Resume | `timetable.resume_job(job_name TEXT) RETURNS boolean` | toggles `chain.live=true` |
| Add one-task job | `timetable.add_job(...)` | chain + task + param in one call |
| Add task | `timetable.add_task(kind, command, parent_id, order_delta DEFAULT 10)` | |
| Reorder | `timetable.move_task_up(task_id)` / `move_task_down(task_id)` | |
| Delete task | `timetable.delete_task(task_id) RETURNS boolean` | |
| Delete job | `timetable.delete_job(job_name) RETURNS boolean` | cascades to tasks |

### 4.3 NOTIFY payload contract

```json
{ "ConfigID": 42, "Command": "START", "Ts": 1750000000, "Delay": 0 }
```

Consumed by `internal/pgengine/notification.go` → `ChainSignal{ConfigID, Command, Ts, Delay}`.
`Command` ∈ {`START`, `STOP`}. Workers `LISTEN` on a channel named exactly `client_name`.

### 4.4 YAML data contract

Reuse `pgengine.YamlConfig` → `[]YamlChain` (`chains:` with inline `Chain`,
`client_name`, `schedule`, `live`, and nested `tasks:` of `YamlTask` with `parameters`).
Export (REQ-010) MUST produce YAML that round-trips through `LoadYamlChains`.

## 5. Acceptance Criteria

- **AC-001**: Given a database with N chains, When the user runs `pgtt chain list`,
  Then all N chains are listed with the columns specified in REQ-002.
- **AC-002**: Given a valid chain id and worker name, When the user runs
  `pgtt chain start <id> --worker <name>`, Then `pgtt` calls
  `timetable.notify_chain_start` and the targeted worker runs the chain exactly once,
  even if the chain's `live` flag is `false`, without changing its schedule or `live`.
- **AC-002b**: Given `pgtt chain start <id>` or `pgtt chain stop <id>` invoked without
  `--worker`, Then the command SHALL fail with a clear error and SHALL NOT send any
  NOTIFY.
- **AC-003**: Given a running chain, When the user runs `pgtt chain stop <id> --worker
  <name>`, Then `pgtt` calls `timetable.notify_chain_stop` and the worker cancels it.
- **AC-004**: Given a chain name, When the user runs `pgtt chain pause <name>`, Then
  `chain.live` becomes `false` and the command reports success.
- **AC-005**: Given a YAML file, When the user runs `pgtt apply file.yaml`, Then chains
  are created via the existing codec; with `--replace` existing chains are overwritten.
- **AC-006**: Given existing chains, When the user runs `pgtt export <id> -f out.yaml`,
  Then `out.yaml` re-imports successfully via `pgtt apply out.yaml`.
- **AC-007**: The system SHALL return machine-readable JSON when `-o json` is supplied
  for any list/show command.
- **AC-008**: Given a destructive command without `--yes` on a non-TTY stream, When
  invoked, Then the command SHALL fail safely without performing the deletion.
- **AC-009**: Given an incompatible schema version, When `pgtt` connects, Then it SHALL
  refuse to run and print the detected vs required version.
- **AC-010**: The system SHALL NOT emit any password substring in stdout/stderr for any
  command, including on connection failure.

## 6. Test Automation Strategy

- **Test Levels**: Unit (command parsing, output formatting, SQL builders),
  Integration (against a real PostgreSQL via testcontainers), End-to-End (CLI invoked
  as a subprocess against a seeded schema).
- **Frameworks**: Go standard `testing` with `testify` (consistent with existing repo
  usage), `testcontainers-go` (already used in `internal/testutils`).
- **Test Data Management**: Provision an ephemeral PostgreSQL container, apply the
  embedded `timetable` schema, seed fixture chains/tasks, tear down per package.
- **CI/CD Integration**: Add `cmd/pgtt/...` to the existing Go test task and
  `golangci-lint run`; the repo already defines Unit Test, Coverage, and Lint tasks.
- **Coverage Requirements**: Maintain or exceed the repository's existing coverage
  expectations for new packages; aim ≥ 80% on the `pgtt` client layer.
- **Performance Testing**: Validate `chain list` and `log tail` remain responsive with
  ≥ 500 chains and a large `execution_log`; assert query bounds (LIMIT/pagination).

## 7. Rationale & Context

- pg_timetable is database-driven; the scheduler is a worker that polls/listens on
  Postgres. Therefore the only complete management surface is the database itself
  (PAT-001). A REST-only client could not even list chains (the REST API exposes only
  liveness/readiness/startchain/stopchain).
- The apparent tension between "SQL-only transport" and "live start/stop" is resolved
  by PAT-002: the REST `/startchain` and `/stopchain` handlers are thin wrappers over
  `timetable.notify_chain_start/stop`. Calling those functions over SQL yields identical
  behavior and removes the need to map `client_name → REST URL` (which is not stored in
  the database). Hence CON-003 loses no capability.
- Living in the same repo (CON-001) maximizes reuse and eliminates schema/codec drift:
  the schema is embedded via `sql_embed.go`, the YAML codec already exists in
  `pgengine/yaml.go`, and connection/config/log packages are ready to reuse (CON-002).
- `viper` is already a dependency (`internal/config`); standardizing on `cobra`+`viper`
  (CON-004) avoids introducing a competing flag library and supports the eventual
  migration of the core away from `go-flags`.
- Scriptable-first (REQ-014) directly addresses the originating pain: managing many
  chains over SSH. The shared internal client (GUD-002, PAT-003) keeps the future
  k9s-style TUI cheap to add without rework.

## 8. Dependencies & External Integrations

### External Systems

- **EXT-001**: PostgreSQL database containing the `timetable.*` schema — primary and
  only integration point in v1.

### Third-Party Services

- **SVC-001**: None required beyond PostgreSQL.

### Infrastructure Dependencies

- **INF-001**: Network reachability from the operator host to the PostgreSQL server
  (the same connectivity pg_timetable itself requires).

### Data Dependencies

- **DAT-001**: `timetable` schema objects (`chain`, `task`, `parameter`,
  `active_session`, `active_chain`, `log`, `execution_log`) and functions
  (`notify_chain_start/stop`, `add_job`, `add_task`, `move_task_*`, `delete_*`,
  `pause_job`, `resume_job`) — provided by the pg_timetable core.

### Technology Platform Dependencies

- **PLT-001**: Go toolchain matching the repository's module (`go.mod`) version.
- **PLT-002**: PostgreSQL driver `pgx/v5` (already used by `pgengine`).
- **PLT-003**: CLI framework `cobra`; configuration `viper` (already in deps).

### Compliance Dependencies

- **COM-001**: No additional regulatory requirements; access control is delegated to
  PostgreSQL roles/privileges.

## 9. Examples & Edge Cases

```bash
# List all chains as a table
pgtt chain list "postgresql://scheduler@db/timetable"

# Trigger chain 42 on worker "worker1", delayed 30s
pgtt chain start 42 --worker worker1 --delay 30s

# Pause and later resume by name
pgtt chain pause "nightly-backup"
pgtt chain resume "nightly-backup"

# Apply chains from YAML, overwriting existing definitions
pgtt apply chains.yaml --replace

# Export two chains to a single YAML file
pgtt export nightly-backup 42 -f out.yaml

# Follow logs for a specific chain across the fleet
pgtt log tail --chain 42

# Machine-readable output for scripting
pgtt chain list -o json | jq '.[] | select(.live==false) | .chain_name'
```

Edge cases:

- **Chain not live (RESOLVED)**: `chain start` performs a one-shot run regardless of
  `live`. The worker's `processAsyncChain` handles a manual `START` via `SelectChain`
  (single chain by id) → `SendChain`, bypassing the `live`-gated scheduled path
  (`SelectChains`/`SelectRebootChains`). This is the intended debugging behavior and
  does not modify the chain's schedule or `live` state. (REQ-005 / AC-002)
- **`--worker` mandatory (RESOLVED)**: `--worker` is REQUIRED for `start` and `stop`
  because the NOTIFY channel is named after `client_name`; without it the signal cannot
  reach the correct worker. `pgtt` SHALL fail fast and send no NOTIFY if it is omitted.
  (REQ-005, REQ-006 / AC-002b)
- **Wrong/unknown worker name**: `notify_chain_start` succeeds at the SQL level but no
  worker listens on that channel; `pgtt` SHOULD warn if the named `client_name` is not
  present in `active_session`.
- **Duplicate NOTIFY**: workers dedupe via TTL (`NotifyTTL`); `pgtt` MUST NOT assume a
  single delivery.
- **Schema absent**: `pgtt` SHALL produce a clear error instructing the user to run a
  pg_timetable instance first (it MUST NOT create the schema itself in v1).
- **No TTY + destructive command without `--yes`**: fail safe (AC-008).

## 10. Validation Criteria

- All Acceptance Criteria (AC-001 … AC-010) pass in the integration test suite.
- `golangci-lint run` passes for `cmd/pgtt/...` and any new internal packages.
- No new endpoints or runtime changes were introduced to the scheduler (CON-006).
- `pgtt export | pgtt apply` round-trip reproduces equivalent chains (AC-006).
- Static analysis confirms no credential strings are logged (SEC-002 / AC-010).
- The internal client layer is consumed by both scriptable commands and is structured
  to be reusable by a future TUI (PAT-003), demonstrated by an interface boundary.

## 11. Related Specifications / Further Reading

- `docs/api.md` — current pg_timetable REST API surface.
- `docs/database_schema.md` and `internal/pgengine/sql/ddl.sql` — schema definitions.
- `internal/pgengine/sql/job_functions.sql` — control-plane SQL functions.
- `internal/pgengine/notification.go` — NOTIFY/`ChainSignal` contract.
- `internal/pgengine/yaml.go`, `docs/yaml-format.md`, `docs/yaml-usage-guide.md` — YAML codec.
- `docs/basic_jobs.md` — `add_job` usage and examples.
