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
| `-o, --output` | `table` | Output format: `table` or `json` |
| `--yes` | `false` | Skip confirmation prompts (use in scripts/CI) |
| `--config` | — | Path to a pgtt config file (YAML, viper format) |
| `-v, --verbose` | `false` | Verbose logging |

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

# Query logs
pgtt log list --chain 42 --client worker1 --limit 50
pgtt log list -o json

# Stream logs live (Ctrl-C to stop)
pgtt log tail
pgtt log tail --chain 42 --client worker1
```

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
