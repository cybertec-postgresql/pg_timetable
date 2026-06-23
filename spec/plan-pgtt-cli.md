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

- [x] **P5-1** DONE: `log tail [--chain] [--client]` — 1-second polling cursor on
      `timetable.log.ts`. NOTE: the spec referenced LISTEN/NOTIFY, but the scheduler
      writes log rows via `CopyFrom` with no associated NOTIFY; there is no log-
      notification channel to hook into. Polling is the correct and clean implementation.
      Filters (client_name, chain via message_data) work identically to `log list`.
      `TestTailLogs_ReceivesNewEntries` + `_FilterByClient` verified. (REQ-013)
- [x] **P5-2** DONE: `TailLogs` returns nil on `ctx.Done()` (tested with immediate
      cancel and timed cancel). No deduplication needed for poll-based tail (no NOTIFY
      dedup race). `TestTailLogs_GracefulCancel` verified. (P5-2)
- [x] **P5-3** DONE: `chain list` enriched with `last_run`, `last_duration_ms`,
      `last_returncode`, `last_worker` via LATERAL subquery on `execution_log`.
      `chain show` also picks up same fields. `TestListChains_EnrichedLastRun` verified.
      (REQ-012)
- [x] **P5-4** DONE: `chain runs <id|name> [--limit N]` — one row per `txid`, grouped
      with `MIN(last_run)`, `MAX(finished)`, `bool_and` status logic, task+failed counts.
      `TestListRuns` + `TestListRuns_LimitRespected` verified. (REQ-012)
- [x] **P5-5** DONE: `chain run-detail <txid>` — per-task rows with command, kind,
      output, params, returncode, start/finish, duration. `TestShowRun` verified. (REQ-012)

**Exit criteria (MET)**: All P5-1…P5-5 done.
`go test ./cmd/pgtt/...` green (12 unit + 31 integration); lint 0 issues.

---

## Phase 6 — Hardening & docs

- [x] **P6-1** DONE: all AC-001…AC-010 mapped to passing tests (see table in plan notes).
      CON-006 confirmed: no scheduler code changed. PAT-003: `_ Client = (*PgClient)(nil)`.
- [x] **P6-2** DONE: `TestListChains_Performance` (500 chains: 34ms), `TestListLogs_Performance`
      (2000 rows, limit 100: 4ms), `TestListRuns_Performance` (limit 10: 6ms). All well
      within 5s ceiling. Pagination bounds (LIMIT) verified. (§6)
- [x] **P6-3** DONE: Moved worker-missing warning from `fmt.Printf` in client layer to
      `cmd.ErrOrStderr()` in command layer (`WorkerExists` now exported on interface).
      Root `Execute` uses `root.ErrOrStderr()` instead of `fmt.Println`. DSN passwords
      already redacted by `redactDSNError` before any error surfaces. (SEC-002 / AC-010)
- [x] **P6-4** DONE: `docs/pgtt.md` — full user reference (connection, flags, all commands
      with examples, completions, config file, schema compatibility). Added to `mkdocs.yml`
      nav under Reference. Cobra built-in `completion` subcommand provides bash/zsh/fish/
      PowerShell shell completions. (§6)
- [x] **P6-5** DONE: `Makefile` with `build`, `build-pgtt`, `build-all`, `release`
      (ldflags for version/commit/date on both binaries), `test`, `lint`, `clean`.
      ldflags vars: `cmd/pgtt/cmd.version`, `.commit`, `.date` matching Dockerfile
      convention. (§6)

**Exit criteria (MET)**: all AC-001…AC-010 green; spec §10 fully satisfied.
`go test ./cmd/pgtt/...` green (12 unit + 34 integration); lint 0 issues; full build OK.

---

## Phase 7 — Log rendering: restore the lost context & style

> **Problem statement.** `log list` and `log tail` produce flat, contextless output. Every
> `timetable.log` row renders `CHAIN=0 TASK=0 MS=0 RC=0` — a wall of zeros — even though
> `timetable.log.message_data` (jsonb) carries the full structured context the scheduler's
> own formatter shows: `chain` (`{ChainID, ChainName}`), `task`, `sql`, `params`, plus
> `vxid`, `notice`, `severity`. The richness already lives in the database; `activitySQL`
> hard-codes `0::bigint AS chain_id/task_id` and never reads `message_data`. There is also
> no color, no level emphasis, and no column width discipline, so long messages stretch the
> table while short rows look ragged.
>
> **Compare** (both captured from the same run):
>
> - `pg_timetable.output` (scheduler, *rich*): `[INFO] [chain:1|notify_every_minute] [task:1] [vxid:21474836598] Starting task`
> - `pgtt.output` (CLI, *flat*): `log  INFO   0  0  0  demo_worker  Chain executed successfully` ← *which* chain?
>
> **Goal.** Make `log list` / `log tail` at least as informative and readable as the
> scheduler's native logger, by mining `message_data` and applying disciplined,
> color-aware rendering — **without touching the scheduler or the DB schema** (CON-006).
>
> **Scope.** `log list` and `log tail` only (and the shared rendering helpers they use).
> `log diag`, `chain *`, `session`, `active` are out of scope except where they share the
> new renderer.

### 7.1 — Mine `message_data` in SQL (data layer)

- [x] **P7-1** DONE: extended `activitySQL` and the `TailActivity` query so the **`log`
      source no longer hard-codes zeros**. Now reads from `message_data`:
      `chain_id ← (message_data->'chain'->>'ChainID')::bigint`,
      `chain_name ← message_data->'chain'->>'ChainName'`,
      `task_id ← (message_data->'task'->>'TaskID')::bigint`,
      `vxid ← message_data->>'vxid'` (text). `exec` rows expose `txid::text AS vxid`.
      All wrapped in `COALESCE(...,0/'')` so rows without a key still render. Key names
      confirmed: scheduler logs `WithField("chain", chain)`/`WithField("task", task)` and
      the structs (`pgengine/types.go`) have **no json tags**, so logrus marshals Go field
      names `ChainID`/`ChainName`/`TaskID`; `vxid` is a top-level int64 field.
- [x] **P7-2** DONE: added `ChainName string` and `Vxid string` to `client.ActivityEntry`
      (with `db`/`json` tags) and **replaced** the old `Txid int64` field — virtual xids
      (`21474836598`/`317827579929`) are not plain ints, so stored as string. `rawRow` in
      `TailActivity` and the emit mapping updated to match. Build + vet clean; no remaining
      references to `ActivityEntry.Txid` (the `.Txid` on `RunSummary` is a separate type).
- [x] **P7-3** DONE: surface notice context for `log` rows. Added `notice`
      (`message_data->>'notice'`) and `severity` (`message_data->>'severity'`) columns to
      both activity queries, the `rawRow` struct, the emit mapping, and `ActivityEntry`
      (`Notice`/`Severity`). The row `level` now uses
      `COALESCE(NULLIF(message_data->>'severity',''), log_level::text)`, so a captured PG
      NOTICE/WARNING reads as `NOTICE`/`WARNING` instead of the bare `INFO` the scheduler
      logs it under (`bootstrap.go` OnNotice: `WithField("severity",…).WithField("notice",…).Info("Notice received")`).
      Empty for non-notice rows. `exec` rows emit `'' AS notice/severity`.
- [x] **P7-4** DONE: integration tests in `pgclient_activity_test.go` —
      `TestListActivity_LogRowContextFromMessageData` (chain id+name, task id, vxid mined
      from `message_data`, demo_worker fixture mirroring `pgtt.output`),
      `TestListActivity_LogRowWithoutContext` (plain row → empty/zero, no extraction error),
      `TestListActivity_FilterByChainMatchesLogRows` (chain filter now matches log rows via
      `message_data`), `TestListActivity_NoticeSeverity` (PG NOTICE → notice+severity
      surfaced, severity drives level), `TestListActivity_SeverityFallsBackToLogLevel`
      (no severity → keeps `log_level`). All green; lint 0 issues.

### 7.2 — A real renderer (presentation layer)

- [x] **P7-5** DONE: added `cmd/pgtt/cmd/logrender.go` with `renderActivityText` — an
      identity-first format `TS LEVEL [chain:id|name] [task:id] [vxid:…] … message`.
      Chain renders as `id|name` (just `id` when name empty); **empty context tokens are
      omitted** (`identityTokens` skips zero/empty chain/task/vxid). `MS`/`RC` tokens are
      emitted **only for `exec` rows** (and `rc` only when non-zero), so log rows no longer
      carry `0 0 0`.
- [x] **P7-6** DONE: dependency-free ANSI helper (`colorize`, `levelColor`) mirroring
      `getColorByLevel` (INFO/OK/NOTICE green, DEBUG/TRACE/RUNNING blue, WARN/WARNING
      magenta, ERROR/FATAL/PANIC/FAIL red); level badge bold, timestamp dimmed.
      `configureLogColor` auto-disables color for `--output json`, `--no-color` (new global
      flag), the `NO_COLOR` env var, and any non-`*os.File`/non-TTY writer — so buffers and
      pipes get clean text. No new dependency added (keeps `pgtt` lean per P0).
- [x] **P7-7** DONE: `clampToWidth` trims the trailing message to terminal width with an
      `…` ellipsis; `visibleLen` measures ignoring ANSI escapes so clamping reflects what
      the user sees. Width comes from `terminalWidth` (TTY + `COLUMNS`, dependency-free);
      returns 0 (no clamping) for pipes, keeping redirected output complete. `--output json`
      stays full/untruncated.
- [x] **P7-8** DONE: `log tail` no longer hand-rolls fixed-width `fmt.Fprintf`. Both
      `log list` (`renderActivityList`) and `log tail` (`newTailEmitter`) go through the
      same `renderActivityText`. The `# pgtt log tail — press Ctrl-C to stop` banner is
      kept (text mode).

### 7.3 — Format selection & polish

- [x] **P7-9** DONE: reused `--output` (no new flag) with a third value `text`. Added
      `outputText` to `parseOutputFormat`; generic `render()` degrades `text`→table so other
      commands are unaffected. Log commands default to `text` unless `--output` was
      explicitly set (`cmd.Flags().Changed("output")` via `effectiveLogFormat`). `log tail`
      honors `text` (live rich) and `json` (NDJSON, one compact object per line); `table`
      is list-only and degrades to text for tail. `json` remains full/untruncated.
- [x] **P7-10** DONE: `docs/pgtt.md` updated. Global-flags table now lists `text` for
      `-o/--output` and the new `--no-color` flag (with a note that `text` falls back to
      `table` for non-log commands and is the `log` default). Added a **Log output formats**
      subsection with `pymdownx.tabbed` tabs (text/table/json), a before/after `pgtt.output`
      comparison, the color palette, empty-token-omission and width-clamp behavior, the
      NDJSON note for `log tail -o json`, and a tip listing every color-disable trigger
      (`-o json` / pipe / `--no-color` / `NO_COLOR`). Verified the required markdown
      extensions (`admonition`, `pymdownx.superfences`, `pymdownx.tabbed`) are enabled in
      `mkdocs.yml`.
- [x] **P7-11** DONE: `cmd/pgtt/cmd/logrender_test.go` — `TestRenderActivityText_Golden`
      (5 cases: log w/ chain+task+vxid, log w/o context, exec OK w/ ms, exec FAIL w/ rc,
      notice→severity-as-level, exact bytes, color off), `TestRenderActivityText_NoZeros`
      (guards no `[chain:0]/[task:0]/[ms:0]/[rc:0]`), `TestConfigureLogColor_Gates` (json /
      --no-color / NO_COLOR / non-TTY), `TestColorize`, `TestClampToWidth`, `TestVisibleLen`,
      `TestActivityRow`, `TestNewTailEmitter_JSON` (NDJSON), `TestNewTailEmitter_TextBanner`.
      All green; `golangci-lint` 0 issues.
- [x] **P7-12** DONE (enhancement): added `tree` output mode (`-o tree`) for `log list`.
      **Grouping/ordering is done entirely in SQL** (`activityTreeSQL` + `ListActivityTree`),
      not in Go — window functions are the right tool. Pipeline: `feed` (same UNION as
      activitySQL, limited to newest $3) → `runseq` (running `sum()` of "Starting chain"
      per chain+client = a temporal run number; never merges distinct runs, and works
      despite the start line carrying no vxid) → `runs` (broadcast the run's real vxid onto
      its vxid-less lines via `max() OVER` partition) → `ranked` (`row_number()=1` ⇒
      `is_header`, `max(ts) OVER` ⇒ run_last). Final `ORDER BY (chain_id=0), run_last DESC,
      …, tree_rank, ts` puts newest run first, "Starting chain" first within a run, chain-less
      rows last. Validated live via psql against the dev DB before coding. `ActivityEntry`
      gained `IsHeader bool` (`db:"is_header"`, `json:"-"`); flat `activitySQL` emits
      `false AS is_header`; `run_vxid` wrapped in COALESCE→'' to avoid NULL scan.
      The renderer (`renderActivityTree`) is now a dumb consumer: blank line + full header on
      `IsHeader`, else `|- ` child with chain/client/vxid suppressed (`tokenOpts.omitVxid`
      added). `log tail` degrades `tree`→`text`.
      `outputTree` added to `parseOutputFormat` (non-log cmds degrade to table). Tests:
      integration `TestListActivityTree_GroupsRunsInSQL` + `_FilterByChain`; unit
      `TestRenderActivityTree_HeaderAndChildren`, `_SystemLinesInterleave`,
      `TestParseOutputFormat_Tree`. Docs: `tree` tab + flags table.
      All green; lint 0 issues (no more gocyclo — grouping left SQL to handle).
      REFINEMENTS (each validated live via psql before coding):
      (a) chain-less system rows **interleave by their own timestamp** between branches
      (`anchor_ts` = own ts), rendered as standalone `|- ` lines with no `(no chain)`
      heading; consecutive system lines stay in one block so they can be spotted in place.
      (b) run grouping switched from a positional `sum()` counter to a **LATERAL lookup of
      the nearest following vxid**, so runs key on the real transaction id — fixes foreign
      rows leaking across overlapping runs / ts ties.
      (c) `tree_rank` forces `Chain executed successfully` LAST (2) and `Starting chain`
      first (0); within-run order is otherwise strict timestamp, with correct tie-breaking.

**Exit criteria (MET)**: `log list`/`log tail` render chain id+name, task, vxid, and notice
context sourced from `message_data`; INFO/WARN/ERROR and exec OK/FAIL are color-coded on a
TTY and plain elsewhere; empty context tokens are omitted (no more `0  0  0` columns);
`--output json` remains complete and untruncated; scheduler and DB schema unchanged
(CON-006). `go build ./cmd/pgtt/...` OK; `golangci-lint run ./cmd/pgtt/...` = 0 issues;
`go test ./cmd/pgtt/cmd/` green; activity integration tests green. All P7-1…P7-11 done.

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
| REQ-012 / REQ-013 (log readability) | P7-1…P7-11 |
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
