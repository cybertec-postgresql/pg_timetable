---
title: pgtt TUI — Phased Implementation Plan
version: 0.1 (draft)
date_created: 2026-06-23
last_updated: 2026-06-23
owner: pg_timetable maintainers
tags: [plan, cli, tui, implementation, checklist]
spec: spec-tool-pgtt-cli.md
parent-plan: plan-pgtt-cli.md
---

# Implementation Plan — pgtt TUI (formerly L-1)

A k9s-style terminal UI for `pgtt`, built **on top of** the existing internal
`client.Client` interface. No data access is re-implemented (PAT-003). This plan
expands the `L-1` item in [`plan-pgtt-cli.md`](./plan-pgtt-cli.md).

## Design decisions (confirmed)

- **Stack**: [Bubble Tea](https://github.com/charmbracelet/bubbletea) +
  `bubbles` + `lipgloss` (Elm-style MVU). Not yet in `go.mod` — added in T0.
- **Launch**: bare `pgtt` (no subcommand) opens the TUI. All existing
  subcommands keep their CLI behaviour. `--dsn`/`--config`/env precedence reused.
- **V1 scope (read + control only — no CRUD/YAML)**:
  - Chains list (home screen)
  - Chain detail (tasks + recent runs)
  - Live activity stream (TailActivity)
  - Sessions / active chains view
  - Control verbs: start / stop / pause / resume
- **Refresh**: auto-refresh timer (configurable interval) **plus** manual `r`.
- **Worker for start/stop**: pick from a list of active sessions
  (`ListSessions`); fail with a status message if none are active.
- **Confirmation**: none — control verbs act immediately on keypress.

## Conventions / DoD

- All data access via `client.Client` only (PAT-003). The TUI imports the
  interface, never raw SQL or `pgengine`.
- New code lives under `cmd/pgtt/internal/tui/`. Package is `tui`.
- DoD per task: `go build ./cmd/pgtt/...` OK, `golangci-lint run
  ./cmd/pgtt/...` = 0 issues, unit tests for pure logic pass.
- No scheduler / DB schema changes (CON-006).
- Timestamps from the client are pre-formatted strings; display as-is.

---

## Phase T0 — Dependencies & launch wiring

- [x] **T0-1** DONE: added `bubbletea v1.3.10`, `bubbles v1.0.0`,
      `lipgloss v1.1.0` via `go get`; `go mod tidy`; build clean.
- [x] **T0-2** DONE: `cmd/pgtt/internal/tui` package — `tui.go` exposes
      `Run(ctx, client.Client, Options) error` (alt-screen, ctx-bound program);
      `Options{Refresh, Host, SchemaVersion, NoColor}`. Imports only
      `client.Client` for data (PAT-003). `model.go` = root MVU model
      (header/body/footer, quit on q/ctrl+c, window-size tracking).
      `styles.go` = lipgloss style set + palette (NoColor strips attrs).
- [x] **T0-3** DONE: root cmd gained `RunE`→`launchTUI` + `Args:
      MaximumNArgs(1)`; subcommands keep own RunE and never reach it.
      `launchTUI` connects (no schema creation) + `CheckSchemaVersion`, keeps
      client open for the session, runs `tui.Run`. Non-TTY stdout → friendly
      hint instead of launching. `tuiTarget` derives a password-free
      host:port/db label from the DSN (SEC-002).
- [x] **T0-4** DONE: `--refresh` duration flag (default 5s) on root persistent
      flags; passed to `tui.Options.Refresh`.

**Exit (MET)**: `pgtt version` unaffected; bare `pgtt` connects + schema-checks
and launches the Bubble Tea shell (header shows host + schema, footer status +
help, quits on q/Ctrl-C); non-TTY prints a hint. `go build ./cmd/pgtt/...` OK;
`golangci-lint run ./cmd/pgtt/...` = 0 issues.

---

## Phase T1 — App shell, navigation & data plumbing

- [x] **T1-1** DONE: `view` interface (`Title/Init/Update/Body/SetSize`) in
      `view.go`; root `model` (`model.go`) owns a `[]view` stack (last = active),
      window size, status + error lines. Navigation via messages the model owns:
      `pushViewMsg`/`popViewMsg`/`replaceRootMsg` (stack ownership in one place).
      Stack seeded lazily via `seedMsg` to dodge the value-receiver `Init` pitfall.
- [x] **T1-2** DONE: refresh engine in `messages.go` — `tickCmd`→`tickMsg`
      reschedules itself and emits `refreshMsg`, routed to the active view. Manual
      `r` emits the same `refreshMsg`. Views fetch via their own `tea.Cmd`
      (never block the UI loop). `--refresh<=0` ⇒ ticker disabled (manual only).
- [x] **T1-3** DONE: `keys.go` global key map via bubbles `key` (Up/Down/Enter/
      Back/Refresh/Help/Quit + `1/c` `2/s` `3/a` top-level switches), wired in
      `model.handleKey`. Help overlay (`help.go`, bubbles `help`): `?` toggles the
      full key grid as the body; `Esc` closes it at root. `ShortHelp`/`FullHelp`
      drive footer + overlay.
- [x] **T1-4** DONE: shared style set + palette in `styles.go`
      (header/footer/selected-row/error, NoColor strips attrs). The level→lipgloss
      `levelColor` mapping + `styles.level()` (mirroring `logrender.go`) landed
      with T2, where the chains status cell consumes it.
- [x] **T1-5** DONE: footer shows status (left) / refresh countdown (right)
      + short-help beneath; `refreshLabel` renders "next refresh in Ns" or
      "refresh: manual". Errors surface in `statusErr` style (DSN already redacted
      by the client layer). Header breadcrumb shows the view-stack path.
- [x] **T1-6** DONE (tests): `model_test.go` exercises seed/stack, quit, top-level
      switch, push/pop + Esc semantics, help toggle, status/err messages, refresh
      label, refreshMsg reaching the active view, and tick reschedule. All green.

**Exit (MET)**: navigable shell with working refresh ticker + countdown, help
overlay, breadcrumb, and `1/2/3` top-level switching across placeholder panes;
push/pop drill-down works. `go build`/`go test ./cmd/pgtt/...` green;
`golangci-lint run ./cmd/pgtt/...` = 0 issues; non-TTY guard intact.

---

## Phase T2 — Chains list (home)

- [x] **T2-1** DONE: `chainsView` (`chains.go`) backed by `client.ListChains`
      (fetched off the UI loop via a `tea.Cmd`). Columns: ID, NAME, LIVE, ACTIVE,
      SCHEDULE (run_at), LAST (status, colored via `styles.level`), LAST RUN,
      WORKER. Generic column-based renderer in `table.go` (flex widths, ellipsis
      truncation, selection highlight + vertical scroll). Status cell uses the
      shared level palette (T1-4 completed: `levelColor`/`styles.level` added).
- [x] **T2-2** DONE: sort cycles id→name→last run (`o`); last-run sorts newest
      first with never-run rows last. Incremental filter box (`/`) matches
      name/client case-insensitively; `inputCapturer` interface routes raw keys
      to the box while active so letters like `q`/`r` edit text instead of
      triggering global bindings. Esc clears.
- [x] **T2-3** DONE: `Enter` pushes a detail view via `pushView`. Until T3, this
      is a placeholder titled with the chain name (swapped for the real detail in
      T3).
- [x] **T2-4** DONE: auto-refresh + manual `r` re-fetch and `reindex`; selection
      is preserved by **chain id** across refreshes (re-sorts/re-filters then
      re-locates the previously selected chain). Verified by unit test.
- [x] **T2-5** DONE (tests): `chains_test.go` covers sort (id/name/last-run),
      filter (name/client/no-match), selection preservation across reorder,
      move-clamping, filter input capture (letter not swallowed as quit), body
      render smoke, and the error→`errMsg` path. All green.

**Exit (MET)**: home screen lists chains (live dev DB verified to return data),
refreshes without losing selection, filters + sorts in-memory, and drills in.
`go build`/`go test ./cmd/pgtt/...` green; `golangci-lint run ./cmd/pgtt/...`
= 0 issues. (Interactive render needs a real TTY; logic is unit-tested.)

---

## Phase T3 — Chain detail (tasks + runs)

- [x] **T3-1** DONE: `detailView` (`detail.go`) from `client.ShowChain(ref)` —
      header block (id, name, live/running, schedule, max, timeout, on_error,
      client) + ordered tasks table (ID, NAME, KIND, COMMAND one-lined, RUN AS,
      FLAGS ign/auto/remote, TIMEOUT). `ref` is the chain id as a string.
- [x] **T3-2** DONE: recent-runs pane from `client.ListRuns(ref, 20)`: TXID,
      STARTED, MS, STATUS (colored via `runStatusLevel`→`styles.level`), TASKS,
      FAILED, CLIENT.
- [x] **T3-3** DONE: `runDetailView` (`rundetail.go`) from `client.ShowRun(txid)`
      — per-task table (TASK, KIND, COMMAND, RC colored, MS, STARTED) on top; the
      selected task's `params` + `output` in a scrollable bubbles `viewport`
      below. ↑/↓ change the selected task (refilling the viewport); pgup/pgdn/
      space scroll the output.
- [x] **T3-4** DONE: stacked split (tasks top / runs bottom) sized from the body
      height (tasks slightly larger), reflows on resize; `Tab` switches focus
      between panes (only the focused pane shows a selection + responds to ↑/↓);
      `Esc` pops back (model-owned stack). `Enter` only opens a run from the runs
      pane.
- [x] **T3-5** DONE (tests): `detail_test.go` (load chain+runs, focus switch +
      per-pane move, Enter opens run-detail only in runs pane with correct txid,
      error→errMsg, `runStatusLevel`, `oneLine`, `clamp`) and `rundetail_test.go`
      (load tasks, body render, ↑/↓ refills viewport with the right task output,
      title, error path). All green.

**Exit (MET)**: full read drill-down chains → tasks/runs → run detail; statuses
colored; output scrollable. Dev DB confirmed to return runs for drill-down.
`go build`/`go test ./cmd/pgtt/...` green; `golangci-lint run ./cmd/pgtt/...`
= 0 issues. (`chains.go` now pushes the real `detailView`; the placeholder
remains only for the not-yet-built Sessions/Activity top-level switches.)

---

## Phase T4 — Live activity stream

- [ ] **T4-1** Activity view backed by `TailActivity` (+ initial `ListActivity`
      backfill). Bridge the synchronous `emit` callback → Go channel → `tea.Msg`
      in a goroutine started as a `tea.Cmd`; cancel via context on view exit.
- [ ] **T4-2** Render each entry using the same identity-first format as the CLI
      (`[chain:id|name] [task:id] [vxid:…]`), colored by level. Reuse/port the
      `identityTokens` model so CLI and TUI stay consistent.
- [ ] **T4-3** Ring buffer (cap N lines) with autoscroll; `f` to freeze/unfreeze
      scroll, `g`/`G` top/bottom.
- [ ] **T4-4** Filters: `--chain`/`--client` style filtering, plus contextual
      launch (open activity pre-filtered to the selected chain from T3).

**Exit**: live, colored, filterable activity stream that starts/stops cleanly
with the view lifecycle.

---

## Phase T5 — Sessions / active chains

- [ ] **T5-1** Sessions view from `ListSessions` (client_name, pids, started_at)
      and active chains from `ListActiveChains` (chain_id, client, started_at),
      shown as two stacked tables or a tabbed pane.
- [ ] **T5-2** Auto-refresh; this view doubles as the worker picker source.

**Exit**: operators can see workers + currently running chains at a glance.

---

## Phase T6 — Control verbs

- [ ] **T6-1** Key-bound verbs on a selected chain: `s` start, `x` stop,
      `p` pause, `u` resume → `StartChain`/`StopChain`/`PauseChain`/`ResumeChain`.
      Pause/resume need no worker; act immediately, then refresh + status toast.
- [ ] **T6-2** Worker picker (bubbles `list`) for start/stop: populate from
      `ListSessions`; if none active, show an error toast and abort. If exactly
      one, preselect it. Validate with `WorkerExists` before sending.
- [ ] **T6-3** `delay` prompt (optional) for start (default 0). Numeric input.
- [ ] **T6-4** Surface command success/failure in the status bar; auto-refresh
      the underlying list so live/active state updates.

**Exit**: full operational control (start/stop/pause/resume) from the TUI with
worker selection, no confirmations.

---

## Phase T7 — Polish, tests & docs

- [ ] **T7-1** Unit tests for pure logic: key map, filter/sort, identity-token
      formatting, level→lipgloss color mapping, ring buffer. (Bubble Tea models
      are testable via `tea.Model.Update` without a real terminal.)
- [ ] **T7-2** Graceful teardown: cancel tail goroutines, `Close()` client on
      quit; restore terminal on panic (`tea.WithAltScreen` + recover).
- [ ] **T7-3** `docs/pgtt.md` (or new `docs/pgtt-tui.md`) — launch, key bindings,
      views, refresh, control verbs, screenshots/ASCII. Add to `mkdocs.yml` nav.
- [ ] **T7-4** Update `plan-pgtt-cli.md` L-1 to DONE and cross-link.

**Exit**: lint clean, tests green, documented, terminal-safe.

---

## Traceability

| Requirement / AC | Phase / Task |
|------------------|--------------|
| PAT-003 (interface reuse) | T0-2, all data fetches |
| REQ-002/003 (chains/detail) | T2, T3 |
| REQ-005/006/007 (start/stop/pause/resume) | T6 |
| REQ-011 (sessions/active) | T5 |
| REQ-012/013 (logs/activity) | T4 |
| CON-006 (no scheduler/schema change) | all |

## Related

- Parent plan: [`plan-pgtt-cli.md`](./plan-pgtt-cli.md) (item L-1)
- Spec: [`spec-tool-pgtt-cli.md`](./spec-tool-pgtt-cli.md)
