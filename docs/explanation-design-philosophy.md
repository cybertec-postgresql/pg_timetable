# Design Philosophy

pg_timetable was created to solve a recurring set of operational frustrations
with existing PostgreSQL schedulers. It is not merely another cron-in-the-database
— it is an opinionated orchestration engine designed around a few core beliefs
about how database automation should work.

## Chains, Not Cron

Traditional schedulers — pg_cron, pgAgent, even OS-level cron — follow the
"fire and forget" model: at a given time, run a given command, done. This works
for `VACUUM` at midnight, but it breaks down as soon as you need *sequencing*:
run an ETL extract, wait, transform, wait, load, wait, then send a success
email. Or, on failure, send an alert instead and skip the subsequent steps.

pg_timetable's fundamental unit is the **chain**: an ordered sequence of tasks
where each step's outcome determines what happens next. A task can point to
another task to run on success, and a different one on error. This turns
scheduling into workflow orchestration — you are not just triggering isolated
statements; you are defining control flow that lives in the database alongside
your data.

The chain model also enables **autonomous tasks**: a step that fires and
detaches, letting the chain continue without waiting. This is not a cron
concept at all — it is an admission that real-world automation often involves
steps whose completion is irrelevant to the parent workflow.

## Database-Driven, Not Database-Locked

Every PostgreSQL scheduler stores job metadata in the database. But most
require a PostgreSQL extension, which means `shared_preload_libraries`,
a server restart, and a dependency on a specific PostgreSQL major version.

pg_timetable is a **standalone Go binary**. It connects to PostgreSQL like any
other client — over libpq, with a connection string. No extension to install,
no server restart, no coupling to a single PostgreSQL version or deployment
model. If you can `psql` into it, pg_timetable can schedule on it.

This architecture has a second, more subtle consequence: **remote database
execution**. A single pg_timetable instance can schedule jobs across multiple
PostgreSQL servers, including servers on different hosts, different versions,
and different cloud providers. Most competitors are either restricted to the
local server (pg_cron, as a background worker) or can reach remote hosts but
require complex setup (pgAgent). pg_timetable treats every connection string as
a peer — a first-class scheduling target with its own chains, its own execution
context, and its own logging namespace.

Being in Go rather than C or C++ also means a single statically-linked binary
that runs identically on Linux, macOS, Windows, and every BSD — no runtime,
no dynamic linking surprises, no compiler toolchain required for deployment.

## Batteries Included

pg_timetable accumulates features that other PostgreSQL schedulers leave to
external tooling. This is deliberate.

**Built-in tasks.** Only pg_timetable ships with predefined operations —
`SendMail`, `Download`, `CopyFromFile`, `Sleep`, `Log`, and more — that are
directly addressable as chain steps. You do not need to escape to a shell
script to send an email when an ETL chain fails; you add a `SendMail` task
with the right parameters and wire it to the error path.

**Task parameters.** Only pg_timetable supports parameterized tasks. A
`my_func($1, $2)` definition can receive different arguments depending on the
chain that invokes it, which enables task reuse across chains without
duplication. Combined with the YAML or JSON `COMMAND_CONFIG` format, this also
means built-in tasks accept structured configuration — not opaque shell
strings.

**Granular logging.** Other schedulers either log to a single destination or
leave logging details undocumented. pg_timetable logs at three levels (session,
job, task) to three destinations simultaneously: stdout/stderr, file, and
database tables. The database logger writes structured rows to
`timetable.log`, which means execution history is queryable with SQL — you can
`SELECT` your scheduler's audit trail with the same tool you use to inspect
your application data.

**Execution control.** Job-level and task-level timeouts prevent runaway
processes. Concurrency protection (`max_instances`) prevents overlapping runs
of the same chain. Manual start and kill operations enable debugging workflows
that most schedulers simply do not support. None of these are glamorous, but
their absence is felt immediately in production — and pg_timetable is the only
PostgreSQL scheduler that provides all of them.

## Operator-First Design

pg_timetable assumes its primary user is a DBA or operations engineer who
already knows cron syntax, already works in SQL, and wants their scheduling
configuration to be as version-controllable and reviewable as their schema
migrations.

**No GUI dependency.** pgAgent requires a graphical interface
to define schedules — cron syntax is not accepted as input. This is a
deliberate inversion: pg_timetable schedules are always expressed as cron
expressions, interval strings, or cron-like tokens (`@reboot`, `@every`),
whether you define them via `timetable.add_job()` in SQL or via a YAML file.
There is no checkbox-to-cron translation layer because there are no checkboxes.

**Config-as-code.** Chains can be defined in SQL (call `add_job()` and
friends) or in YAML files loaded at startup. Both are plain text, both can
live in version control, both can be reviewed in a pull request. The YAML path
is the recommended approach for production: it separates scheduling
configuration from runtime state, makes bulk updates safe, and integrates
naturally with CI/CD pipelines.

**Scheduling richness.** pg_timetable is the only PostgreSQL scheduler
supporting standard cron, interval-based scheduling, `@reboot`
execution, manual start, manual kill, job timeouts, task timeouts, job
disabling, and self-destructive (run-once-then-delete) jobs — all
simultaneously. This is not feature checklisting; it reflects a philosophy that
scheduling is a spectrum of operational needs, and the tool should not force
you to switch to another scheduler just because your use case changed from
"run every Tuesday" to "run once when I restart the scheduler" to "run now
because I'm debugging."

## What pg_timetable Is Not

pg_timetable is not a distributed workflow engine. It does not coordinate
chains across multiple scheduler instances, it does not implement leader
election, and it does not provide a distributed lock manager. If you need
horizontally-scaled job execution with failover, you are looking for something
closer to Apache Airflow or Temporal. pg_timetable is designed for the
single-scheduler-per-database-cluster operational model that covers the vast
majority of PostgreSQL deployments.

pg_timetable is not a replacement for OS-level cron when the task has nothing
to do with the database. If you are scheduling filesystem cleanup or system
reboots, use cron. pg_timetable earns its complexity when the scheduled work
touches PostgreSQL — directly (SQL tasks), indirectly (shell tasks that connect
back to PostgreSQL), or operationally (built-in tasks that depend on database
state).

pg_timetable is not a monitoring system. It has OpenTelemetry export and
database-level logging, but it does not replace Prometheus alert rules or
Grafana dashboards. It tells you what it ran and whether it succeeded; you
bring the alerting.

## Why Go? Why Standalone?

The choice of Go as the implementation language was not arbitrary. A
PostgreSQL scheduler must manage concurrent database connections, long-running
task goroutines, timeout enforcement, and signal handling — all of which map
naturally to Go's concurrency primitives. A single statically-linked binary
eliminates the runtime dependency issues that plague C/C++ schedulers (libpq
version drift, compiler ABI changes) and the JVM footprint concerns that make
Java-based schedulers heavy for a sidecar process.

The standalone architecture — as opposed to a PostgreSQL background worker —
means pg_timetable can be deployed as a systemd service, a Docker container, a
Kubernetes sidecar, or a Nomad job without touching the PostgreSQL server
configuration. It also means the scheduler can be restarted, upgraded, or moved
to a different host without affecting the database server — a property that
matters when the database is a managed cloud service where you cannot modify
`shared_preload_libraries`.

## Further Reading

- [The Scheduling Model](explanation-scheduling-model.md) — the three-tier
  abstraction (command → task → chain) and how they compose
- [Commands, Tasks, and Chains Reference](reference-commands-tasks-chains.md)
  — the exact fields and parameter formats for each tier
- [PostgreSQL schedulers: comparison table](https://www.cybertec-postgresql.com/en/postgresql-schedulers-comparison-table/)
  — the feature matrix that motivated many of these design decisions
- [CERN talk: PostgreSQL Job Scheduling](https://cds.cern.ch/record/2706921)
  — Pavlo Golub's 2020 presentation on the landscape that pg_timetable was
  designed for