---
title: pgtt — Phased Implementation Plan
version: 0.1 (draft)
date_created: 2026-06-23
last_updated: 2026-06-23
owner: pg_timetable maintainers
tags: [plan, cli, implementation, checklist]
spec: spec-tool-pgtt-cli.md
---

# Implementation Plan — pgtt

Companion to [`spec-tool-pgtt-cli.md`](./spec-tool-pgtt-cli.md). Each task references
the spec requirement (`REQ-`/`SEC-`/`CON-`) and/or acceptance criterion (`AC-`) it
satisfies, so progress maps directly back to the spec. Check items off as completed.

## Conventions

- **DoD (Definition of Done)** for every phase: code compiles, `golangci-lint run`
  clean for touched packages, unit/integration tests for the phase's ACs pass, and the
  spec's relevant requirement IDs are referenced in commits/PR.
- Keep all data access behind the internal client layer (GUD-002 / PAT-003).
- Prefer existing `timetable.*` SQL functions over ad-hoc DML (GUD-001).

---

## Phase 0 — Decisions & groundwork

Resolve open questions and lay the package skeleton. No user-facing features yet.

- [x] **P0-1** RESOLVED: `chain start` is a one-shot debug run that executes the chain
      exactly once regardless of `live`, and does not change schedule/`live`.
      (REQ-005 / AC-002) — confirmed against `scheduler.processAsyncChain`.
- [x] **P0-2** RESOLVED: `--worker` is MANDATORY for `start`/`stop` (NOTIFY channel ==
      `client_name`); fail fast and send no NOTIFY if omitted. (REQ-005, REQ-006 / AC-002b)
- [x] **P0-3** DONE: Go 1.25.0, module `github.com/cybertec-postgresql/pg_timetable`.
      Added `github.com/spf13/cobra v1.10.2`; `viper v1.21.0` already present.
      (CON-004, PLT-001, PLT-003)
- [x] **P0-4** DONE: `cmd/pgtt/main.go` + `cmd/pgtt/cmd/{root,version}.go`; cobra root
      with global flags (`--dsn -o/--output --yes --config -v`), `version` subcommand,
      viper precedence (flags>env PGTT_*>file). `go build ./cmd/pgtt` OK; `go vet` OK;
      `golangci-lint run ./cmd/pgtt/...` = 0 issues. (CON-001, CON-005, REQ-014/015)
- [x] **P0-5** DONE: package builds/vets/lints clean and is picked up by the repo's
      `go test ./...` / `golangci-lint run` tasks (path-globbed, no task edits needed). (§6)

**Exit criteria (MET)**: `pgtt` binary builds, runs `--help` and `version`
(prints compatible DB schema 00733); vet + lint clean.

> Phase 1 note: `pgengine.New` executes/creates the schema on connect. `pgtt` MUST NOT
> create the schema (REQ-016 / §9 "schema absent"); Phase 1 (P1-4) needs a lighter
> connect path that only opens a pool + runs `CheckNeedMigrateDb`-style version check.

---

## Phase 1 — Connection, config & internal client foundation

The reusable core that every later phase depends on.

- [x] **P1-1** DONE: cobra root + viper precedence (flags > env PGTT_* > file) in
      `root.go`/`initConfig`. (CON-004, REQ-015)
- [x] **P1-2** DONE: reuses `internal/pgengine` domain types + embedded schema (via
      testutils) and pgx; light connect path (pgxpool, no schema creation). (CON-002)
- [x] **P1-3** DONE: DSN from `--dsn` > positional arg > `PGTT_CONNSTR` > libpq env;
      `redactDSNError` + generic invalid-DSN msg ensure no password leak. (SEC-001/002 / AC-010)
- [x] **P1-4** DONE: `CheckSchemaVersion` queries `timetable.migration` (latest row),
      compares leading token to `dbSchema`; sentinel `ErrSchemaAbsent` /
      `ErrSchemaIncompatible`; absent => "run pg_timetable first". (REQ-016 / AC-009)
- [x] **P1-5** DONE: `cmd/pgtt/internal/client` `Client` interface (connect/close,
      version check + Phase 2-5 method signatures); `PgClient` impl; `_ Client =
      (*PgClient)(nil)`. (GUD-002, PAT-003)
- [x] **P1-6** DONE: `output.go` (`parseOutputFormat`, `renderTable`/`renderJSON`) +
      `confirm.go` (`--yes`, TTY detection, non-TTY fail-safe). (REQ-015, SEC-003, AC-008)
- [x] **P1-7** DONE: client tests use `testutils.SetupPostgresContainer`; AC-009 (3
      cases) + AC-010 (2 cases) pass; cmd unit tests for output/confirm. (§6)

**Exit criteria (MET)**: AC-009 + AC-010 verified by passing integration tests;
`pgtt check` connects, validates schema, leaks nothing. build+vet+lint clean,
`go test ./cmd/pgtt/...` green. Added `check` subcommand as the e2e exercise.

---

## Phase 2 — Read & observe (highest immediate value over SSH)

Delivers the core pain relief: see everything without crafting SQL.

- [x] **P2-1** DONE: `chain list` — all required columns incl. derived `active`
      (timetable.active_chain) + `last_status` (timetable.execution_log). Fixed pgx
      `RowToStructByName` issue: `db:"-"` tags on derived fields prevented scanning;
      changed to `db:"active"` / `db:"last_status"`. (REQ-002 / AC-001)
- [x] **P2-2** DONE: `chain show <id|name>` — resolves by numeric id or name, returns
      chain + ordered tasks. (REQ-003)
- [x] **P2-3** DONE: `session list` (timetable.active_session) + `active list`
      (timetable.active_chain). (REQ-011)
- [x] **P2-4** DONE: `log list` with `--chain`, `--client`, `--limit` filters;
      default limit 100 for pagination bounds. (REQ-012)
- [x] **P2-5** DONE: `-o json` via `render()` dispatcher for all read commands;
      JSON test in output_test.go. (REQ-015 / AC-007)

**Exit criteria (MET)**: AC-001, AC-007 pass; all read commands work in table + JSON.
`go test ./cmd/pgtt/...` green (11 integration + 7 unit tests); lint 0 issues.

---

## Phase 3 — Live control

Trigger/cancel/pause/resume — the day-to-day operational verbs.

- [x] **P3-1** DONE: `chain start <id> --worker [--delay]` → `notify_chain_start`;
      one-shot regardless of `live` (P0-1 decision verified in test). (REQ-005 / AC-002)
- [x] **P3-2** DONE: `chain stop <id> --worker` → `notify_chain_stop`. (REQ-006 / AC-003)
- [x] **P3-3** DONE: `chain pause/resume` → `pause_job`/`resume_job`; asserts
      `chain.live` toggle in integration test. (REQ-007 / AC-004)
- [x] **P3-4** DONE: `workerExists` queries `timetable.active_session`; prints
      warning but does NOT block the NOTIFY (spec says SHOULD warn, §9). (§9)
- [x] **P3-5** DONE: `--worker` empty → `errWorkerRequired` before any DB connection;
      unit tests `TestChainStart/Stop_WorkerRequired` prove no NOTIFY is sent. (AC-002b)

**Exit criteria (MET)**: AC-002, AC-003, AC-004 pass; AC-002b unit-tested.
`go test ./cmd/pgtt/...` green (18 integration + 9 unit tests); lint 0 issues.

---

## Phase 4 — CRUD & YAML

Full authoring of chains/tasks plus import/export.

- [x] **P4-1** DONE: `chain create` / `chain edit` (Changed()-aware, nil fields skipped)
      / `chain delete` (confirm() guard). (REQ-004, SEC-003 / AC-008)
- [x] **P4-2** DONE: `chain task add/edit/delete` and `chain task move {up|down}`;
      add appends after highest task_order; move uses `move_task_up/down`. (REQ-004, REQ-008)
- [x] **P4-3** DONE: `apply` reuses `pgengine.ParseYamlFile` + `ValidateChain` +
      `SetDefaults`; insert mirrors `CreateChainFromYaml`. (REQ-009 / AC-005)
- [x] **P4-4** DONE: `export` always succeeds; prepends `exportWarningHeader` YAML
      comment + prints per-chain warnings to stderr. No per-pattern classification.
      (REQ-010 / spec §9)
- [x] **P4-5** DONE: `TestApplyExportRoundTrip` — inline static YAML → apply → export
      → re-apply --replace → chain still present with correct schedule. (AC-006)

**Exit criteria (MET)**: AC-005, AC-006, AC-008 pass; round-trip verified on static chain.
`go test ./cmd/pgtt/...` green (12 unit + 23 integration); lint 0 issues.

---

## Phase 5 — Log follow / tail

Streaming observability via LISTEN/NOTIFY.

- [ ] **P5-1** `log tail [--chain] [--client]` using LISTEN/NOTIFY per
      `internal/pgengine/notification.go`. (REQ-013)
- [ ] **P5-2** Graceful shutdown (Ctrl-C), dedupe awareness (NotifyTTL). (§9)

**Exit criteria**: `log tail` streams new entries live and exits cleanly.

---

## Phase 6 — Hardening & docs

- [ ] **P6-1** Validation pass against spec §10 checklist.
- [ ] **P6-2** Performance check: ≥500 chains + large `execution_log` responsive. (§6)
- [ ] **P6-3** Static check: no credential strings logged. (SEC-002 / AC-010)
- [ ] **P6-4** User docs (`docs/`) + shell-completion generation (cobra).
- [ ] **P6-5** Tag/release pgtt as part of the repo build artifacts.

**Exit criteria**: all AC-001…AC-010 green; spec §10 fully satisfied.

---

## Later phase (out of v1 scope) — TUI

- [ ] **L-1** k9s-style TUI (bubbletea/tview) on top of the Phase-1 `Client` interface.
      No re-implementation of data access. (PAT-003)

---

## Traceability matrix

| Requirement / AC | Phase / Task |
|------------------|--------------|
| REQ-001          | P1-2 |
| REQ-002 / AC-001 | P2-1 |
| REQ-003          | P2-2 |
| REQ-004          | P4-1, P4-2 |
| REQ-005 / AC-002 | P3-1 |
| AC-002b           | P3-5 |
| REQ-006 / AC-003 | P3-2 |
| REQ-007 / AC-004 | P3-3 |
| REQ-008          | P4-2 |
| REQ-009 / AC-005 | P4-3 |
| REQ-010 / AC-006 | P4-4, P4-5 |
| REQ-011          | P2-3 |
| REQ-012          | P2-4 |
| REQ-013          | P5-1 |
| REQ-014          | P1-1, all command tasks |
| REQ-015 / AC-007 | P1-6, P2-5 |
| REQ-016 / AC-009 | P1-4 |
| SEC-001/002 / AC-010 | P1-3, P6-3 |
| SEC-003 / AC-008 | P1-6, P4-1 |
| CON-001/005      | P0-4 |
| CON-002          | P1-2 |
| CON-004          | P0-3, P1-1 |
| PAT-003          | P1-5, L-1 |

## Related

- Spec: [`spec-tool-pgtt-cli.md`](./spec-tool-pgtt-cli.md)
