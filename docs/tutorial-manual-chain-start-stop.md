# Starting and Stopping Chains Manually

In this tutorial we'll run pg_timetable in debug mode — where all automatic scheduling is disabled
and chains only fire when you trigger them by hand. This is the workflow you'll use during
development, debugging, and test runs.

1. Make sure you completed [Your First Scheduled Chain](tutorial-first-chain.md) — we'll reuse
   the same `scheduler` role and `my_database`.

2. Create a chain that sleeps for 30 seconds. This gives us time to stop it mid-flight:

    ```sql
    SELECT timetable.add_job(
        job_name          => 'sleepy-chain',
        job_schedule      => '* * * * *',
        job_command       => 'SELECT pg_sleep(30)',
        job_live          => TRUE,
        job_ignore_errors => TRUE);
    ```

    ```text
     add_job
    ---------
           5
    (1 row)
    ```

    The output is your **chain ID** — you'll need it below. Yours may differ; substitute it
    wherever you see `5` in the examples.

3. Run **pg_timetable** with the `--debug` flag:

    ```bash
    pg_timetable --debug --clientname=debugger postgresql://scheduler:somestrong@localhost/my_database
    ```

    ```text
    2024-01-01 12:00:00.000 [LOG]: Connection established...
    2024-01-01 12:00:00.010 [LOG]: Configuration schema created...
    2024-01-01 12:00:00.020 [LOG]: Accepting asynchronous chains execution requests...
    ```

    Notice what's missing: no "Checking for task chains", no "Number of chains to be executed".
    `--debug` skips the entire scheduling loop — cron expressions, intervals, and `@reboot` are
    all ignored. The scheduler only listens for your manual commands.

4. Start the chain by hand:

    ```sql
    SELECT timetable.notify_chain_start(
        chain_id    => 5,
        worker_name => 'debugger');
    ```

    ```text
     notify_chain_start
    --------------------

    (1 row)
    ```

    Back in the **pg_timetable** terminal:

    ```text
    2024-01-01 12:05:00.000 [LOG]: Adding asynchronous chain to working queue: {5 START …}
    2024-01-01 12:05:00.010 [LOG]: Starting chain ID: 5; configuration ID: 5
    2024-01-01 12:05:30.020 [LOG]: Executed successfully chain ID: 5; configuration ID: 5
    ```

    The chain ran **once** and stopped — manual starts always execute a single run, regardless
    of what `job_schedule` says.

5. Now start it again, and this time stop it mid-flight. From your SQL client, fire the chain:

    ```sql
    SELECT timetable.notify_chain_start(5, 'debugger');
    ```

    Wait a few seconds, then cancel it:

    ```sql
    SELECT timetable.notify_chain_stop(5, 'debugger');
    ```

    ```text
     notify_chain_stop
    --------------------

    (1 row)
    ```

    The **pg_timetable** terminal shows the cancellation:

    ```text
    2024-01-01 12:07:00.000 [LOG]: Adding asynchronous chain to working queue: {5 START …}
    2024-01-01 12:07:00.010 [LOG]: Starting chain ID: 5; configuration ID: 5
    2024-01-01 12:07:08.050 [LOG]: Adding asynchronous chain to working queue: {5 STOP …}
    2024-01-01 12:07:08.051 [ERROR]: Task execution failed: …; Error: read tcp …: i/o timeout
    2024-01-01 12:07:08.052 [LOG]: Executed successfully chain ID: 5; configuration ID: 5
    ```

    The task error is expected — you interrupted `pg_sleep`. The chain itself still reports
    success because we set `job_ignore_errors => TRUE` in step 2.

6. PROFIT! You can now start and stop chains on demand.

A few things to remember:

- `--debug` disables **all** automatic scheduling. Chains with `run_at`, `@every`, or
  `@reboot` will not fire unless you trigger them manually. This is the cleanest way to
  isolate a single chain during development.
- In production, you can get the same effect for a single chain without `--debug`: set
  `live = FALSE` on its config, then start it manually when needed. A disabled chain still
  responds to `notify_chain_start`.
- `notify_chain_start` takes a **chain execution config ID** — but `add_job()` creates one
  config per chain, so the numbers match until you add additional configs.
- `worker_name` must match the `--clientname` of a running **pg_timetable** instance.
  If no client with that name is connected, the notification is silently dropped.
- Manual starts accept an optional `start_delay` — see the
  [Commands, Tasks, and Chains Reference](reference-commands-tasks-chains.md) for details.

Ready-to-run examples with longer chains and error handlers live in
[Samples](samples.md) — try `samples/basic.sql` and
[`samples/DelayedRetry.sql`](https://github.com/cybertec-postgresql/pg_timetable/blob/master/samples/DelayedRetry.sql).