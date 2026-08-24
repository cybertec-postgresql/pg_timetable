# Limit Database Sessions When Running Many Jobs

If you're scheduling many chains and need to control how many database connections
**pg_timetable** opens, use the settings below instead of guessing.

## Calculate the connection ceiling

One **pg_timetable** instance opens at most:

```text
max_conn = --cron-workers + --interval-workers + 3
```

The `+3` covers the regular chain receiver, the interval chain receiver, and the database
logger — each holds its own connection alongside the worker pool. With the defaults
(`--cron-workers=16 --interval-workers=16`) that's 35 connections per instance; running several
instances against the same server multiplies that ceiling by the instance count.

To limit sessions, lower `--cron-workers` and/or `--interval-workers` (see the
[Command-Line Reference](reference-cli-options.md)):

```bash
pg_timetable postgresql://scheduler:somestrong@localhost/my_database --cron-workers=4 --interval-workers=2
```

## Reduce concurrent chain executions without touching workers

`job_max_instances` (or `max_instances` in YAML) caps how many copies of *one specific chain*
run at the same time — it does nothing for chains that already run once and self-destruct, since
there's only ever one instance of those in flight. To cut the number of *simultaneously running*
chains overall, without lowering worker counts:

- schedule chains less aggressively, e.g. every 10 minutes instead of every minute;
- use an `@after` schedule instead of `@every` for chains whose next run should wait until the
  previous one finished, rather than firing on a fixed clock regardless of overlap.

For the full field list, see [Commands, Tasks, and Chains Reference](reference-commands-tasks-chains.md#table-timetablechain).

## If you see "Failed to send chain to the execution channel"

This means every worker was busy when the scheduler tried to hand off a newly due chain, and the
in-memory queue between the scheduler and the workers was full. Since the queue is generously
buffered, this indicates a sustained backlog rather than a brief spike — the fixes are the same
levers as above: fewer, less frequent chains, or more workers if the database can take the extra
connections.

## Monitor before you tune

Check `timetable.log` for this error and general execution health, and cross-reference
`pg_stat_activity` against your calculated `max_conn` to confirm the ceiling holds in practice:

```sql
SELECT count(*) FROM pg_stat_activity WHERE application_name = 'pg_timetable';
```
