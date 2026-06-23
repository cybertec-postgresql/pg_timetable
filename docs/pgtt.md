# pgtt — CLI Management Tool

`pgtt` is a command-line management tool for **pg_timetable**. It connects directly
to PostgreSQL and lets you inspect, create, edit, control, and observe scheduler
chains and tasks without crafting SQL manually over an SSH session.

## Installation

Build from source (requires Go ≥ 1.25):

```bash
go build -o pgtt ./cmd/pgtt
```

Or with version information baked in:

```bash
go build \
  -ldflags "-X github.com/cybertec-postgresql/pg_timetable/cmd/pgtt/cmd.version=1.0.0 \
            -X github.com/cybertec-postgresql/pg_timetable/cmd/pgtt/cmd.commit=$(git rev-parse --short HEAD) \
            -X github.com/cybertec-postgresql/pg_timetable/cmd/pgtt/cmd.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -o pgtt ./cmd/pgtt
```

## Connection

`pgtt` accepts a PostgreSQL connection string in three ways, in decreasing priority:

1. `--dsn` flag: `pgtt --dsn "postgresql://user:pass@host/db" chain list`
2. First positional argument: `pgtt chain list "postgresql://user:pass@host/db"`
3. Environment variable `PGTT_CONNSTR`
4. Standard libpq environment variables (`PGHOST`, `PGUSER`, `PGPASSWORD`, `PGDATABASE`, etc.)

Passwords are **never** echoed in error messages or output.

## Global flags

| Flag | Default | Description |
|------|---------|-------------|
| `--dsn` | — | PostgreSQL connection string |
| `-o, --output` | `table` | Output format: `text`, `tree`, `table` or `json` |
| `--no-color` | `false` | Disable colored output (also honors the `NO_COLOR` env var) |
| `--yes` | `false` | Skip confirmation prompts (use in scripts/CI) |
| `--config` | — | Path to a pgtt config file (YAML, viper format) |
| `-v, --verbose` | `false` | Verbose logging |

!!! note "`text` vs `table`"
    `text` is the rich, identity-first rendering used by the `log` commands — see
    [Log output formats](#log-output-formats). For every other command, `text`
    behaves like `table`. The `log list` / `log tail` commands default to `text`
    unless you pass `-o table` or `-o json` explicitly.

## Commands

### Observability

```bash
# Verify connection and schema version
pgtt check

# List all chains (fleet-wide, with last-run summary)
pgtt chain list
pgtt chain list -o json | jq '.[] | select(.live==false) | .ChainName'

# Show a chain and its tasks (by id or name)
pgtt chain show 42
pgtt chain show "nightly-backup"

# Show recent execution runs for a chain
pgtt chain runs "nightly-backup"
pgtt chain runs 42 --limit 5

# Drill into a single run (per-task output, returncode, params)
pgtt chain run-detail 7842391

# Active scheduler sessions and running chains
pgtt session list
pgtt active list

# Unified activity feed (execution results + scheduler messages)
pgtt log list
pgtt log list --chain 42 --client worker1 --limit 50
pgtt log list -o tree    # group lines by chain (great for parallel chains)
pgtt log list -o table   # legacy aligned columns
pgtt log list -o json

# Stream activity live: task completions + scheduler events (Ctrl-C to stop)
pgtt log tail
pgtt log tail --chain 42 --client worker1
pgtt log tail -o json    # NDJSON stream, one object per line

# Raw scheduler diagnostic messages only (timetable.log)
pgtt log diag --limit 50
```

#### Log output formats

The `log list` and `log tail` commands draw their context — chain id + name,
task id, virtual transaction id (`vxid`), and PostgreSQL `NOTICE`/`WARNING`
severity — directly from `timetable.log.message_data` and
`timetable.execution_log`. The default **`text`** format is identity-first and
color-coded, mirroring the scheduler's own log output, instead of a flat table
of zeros.

=== "text (default)"

    ```text
    2026-06-23 16:08:43.385 INFO    [client:demo_worker] Retrieve scheduled chains to run
    2026-06-23 16:08:43.387 INFO    [chain:1|notify_every_minute] [task:1] [vxid:21474836598] [client:demo_worker] Starting task
    2026-06-23 16:08:43.398 NOTICE  [client:demo_worker] Notice received
    2026-06-23 16:08:43.398 OK      [chain:1|notify_every_minute] [task:1] [vxid:21474836598] [ms:7] [client:demo_worker] SELECT 1
    ```

    - Levels/statuses are colored on a terminal: `INFO`/`OK`/`NOTICE` green,
      `DEBUG`/`RUNNING` blue, `WARN`/`WARNING` magenta, `ERROR`/`FAIL` red.
    - Empty context is **omitted** — log rows never show `[chain:0]`/`[task:0]`,
      and `[ms:…]`/`[rc:…]` appear only on execution rows where they matter.
    - Long messages are clamped to the terminal width with an `…` ellipsis;
      redirected/piped output is never clamped.

=== "table"

    ```text
    TS                       SRC   LEVEL   CHAIN                  TASK  MS  RC  CLIENT       MESSAGE
    2026-06-23 16:08:43.398  exec  OK      1|notify_every_minute  1     7   0   demo_worker  SELECT 1
    2026-06-23 16:08:43.387  log   INFO    1|notify_every_minute        0       demo_worker  Starting task
    ```

    The legacy aligned columns (`-o table`). `MS`/`RC` are blank for scheduler
    log rows and populated for execution rows.

=== "tree"

    ```text
    2026-06-23 16:37:43.308 INFO    [chain:1|notify_every_minute] [client:demo_worker] Starting chain
    |- 2026-06-23 16:37:43.340 INFO    [chain:1|notify_every_minute] [task:1] [vxid:270582939710] [client:demo_worker] Closing standalone session
    |- 2026-06-23 16:37:43.340 INFO    [chain:1|notify_every_minute] [task:1] [vxid:270582939710] [client:demo_worker] Task executed successfully
    |- 2026-06-23 16:37:43.341 INFO    [chain:1|notify_every_minute] [vxid:270582939710] [client:demo_worker] Chain executed successfully
    ```

    `tree` (`-o tree`) groups every line of a chain together — the header line
    plus its tasks/events indented with `|-` — **deliberately breaking the global
    time order**. This is invaluable when several chains (or several parallel
    instances of the same chain) run at once and their lines would otherwise be
    interleaved. Within a group, lines are ordered chronologically so a run reads
    top-to-bottom; groups are separated by a blank line and the same chain on a
    different worker is its own group. Scheduler-level lines with no chain are
    collected under a trailing `(no chain)` group. Tree mode applies to
    `log list` only — `log tail` streams, so it uses `text`.

=== "json"

    ```json
    {
      "ts": "2026-06-23 16:08:43.387",
      "source": "log",
      "client_name": "demo_worker",
      "chain_id": 1,
      "chain_name": "notify_every_minute",
      "task_id": 1,
      "vxid": "21474836598",
      "level": "INFO",
      "message": "Starting task"
    }
    ```

    `log list -o json` emits one indented array; `log tail -o json` streams
    **NDJSON** (one compact object per line) for piping into `jq` or a log
    pipeline. JSON output is always complete and never truncated or colored.

!!! tip "Disabling color"
    Color is emitted only on an interactive terminal. It is automatically
    disabled for `-o json`, when output is piped/redirected, when `--no-color`
    is passed, or when the [`NO_COLOR`](https://no-color.org/) environment
    variable is set.

**Before / after.** Previously the same feed rendered every scheduler row as a
wall of zeros with no way to tell which chain a message belonged to:

```text
2026-06-23 16:08:43.387  log   INFO                0   0   demo_worker  Starting task
2026-06-23 16:08:43.398  log   INFO                0   0   demo_worker  Notice received
```

Now the chain, task, vxid and severity context is restored from the data that
was already in the database.

### Live control

```bash
# Trigger a chain once immediately (ignores live flag — useful for debugging)
pgtt chain start 42 --worker worker1
pgtt chain start 42 --worker worker1 --delay 30   # delay 30 seconds

# Cancel a running chain
pgtt chain stop 42 --worker worker1

# Pause / resume scheduled execution
pgtt chain pause "nightly-backup"
pgtt chain resume "nightly-backup"
```

!!! note "--worker is required for start and stop"
    The `NOTIFY` signal is delivered to the scheduler identified by `--worker`
    (its `client_name`). If you omit it, `pgtt` fails immediately without
    sending any notification. Use `pgtt session list` to find active worker names.

### Authoring chains & tasks

```bash
# Create a one-task chain
pgtt chain create --name "daily-vacuum" --schedule "0 3 * * *" --command "VACUUM" --live

# Edit chain attributes (only the flags you supply are changed)
pgtt chain edit "daily-vacuum" --schedule "0 4 * * *" --max-instances 2

# Delete a chain (and all its tasks)
pgtt chain delete "daily-vacuum"           # prompts if interactive
pgtt chain delete "daily-vacuum" --yes     # skip prompt (CI)

# Manage tasks within a chain
pgtt chain task add "daily-vacuum" --command "ANALYZE" --kind SQL
pgtt chain task edit 99 --command "ANALYZE verbose"
pgtt chain task move 99 up
pgtt chain task delete 99 --yes

# Import chains from a YAML file
pgtt apply chains.yaml
pgtt apply chains.yaml --replace           # overwrite existing chains

# Export chains to YAML (best-effort static snapshot)
pgtt export "nightly-backup"
pgtt export 42 "daily-vacuum" -f out.yaml
```

!!! warning "YAML export is a static snapshot"
    `pgtt export` captures the current database rows. It **cannot** reproduce
    chains that are programmatically generated or modify their own tasks/parameters
    at runtime. The exported file includes a warning comment. Review before re-applying.

## Shell completions

`pgtt` uses [cobra](https://github.com/spf13/cobra) and inherits its built-in
completion support:

```bash
# Bash
pgtt completion bash > /etc/bash_completion.d/pgtt

# Zsh
pgtt completion zsh > "${fpath[1]}/_pgtt"

# Fish
pgtt completion fish > ~/.config/fish/completions/pgtt.fish

# PowerShell
pgtt completion powershell | Out-String | Invoke-Expression
```

## Configuration file

`pgtt` accepts a YAML config file (via `--config` or `PGTT_CONFIG` env var).
All global flags can be set there:

```yaml
dsn: "postgresql://scheduler@localhost/timetable?sslmode=disable"
output: table
verbose: false
```

## Schema compatibility

`pgtt` checks the `timetable.migration` table on every connection and refuses to
operate against an incompatible schema version (printed as `required: 00733`).
If the schema is absent, run a pg_timetable instance against the database first
to create and initialise it.
