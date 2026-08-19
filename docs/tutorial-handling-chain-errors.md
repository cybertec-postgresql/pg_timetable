# Handling Chain Errors

In this tutorial we'll schedule a chain that always fails, attach an error handler that logs the
failure and schedules a retry with a growing delay, and watch it back off over several attempts.

1. Make sure you completed [Your First Scheduled Chain](tutorial-first-chain.md) — we'll reuse
   the same `scheduler` role and `my_database`.

2. Create the retry handler. It counts how many times `flaky-job` has failed recently and
   reschedules it after a delay that doubles on every attempt:

    ```sql
    my_database=> CREATE OR REPLACE FUNCTION retry_with_backoff() RETURNS void
    my_database-> LANGUAGE plpgsql AS $$
    my_database-> DECLARE
    my_database->     attempts integer;
    my_database->     delay interval;
    my_database-> BEGIN
    my_database->     SELECT count(*) INTO attempts
    my_database->     FROM timetable.execution_log
    my_database->     WHERE chain_id = current_setting('pg_timetable.current_chain_id')::bigint
    my_database->       AND returncode != 0
    my_database->       AND finished > now() - interval '10 minutes';
    my_database->
    my_database->     IF attempts >= 3 THEN
    my_database->         RAISE NOTICE 'Giving up after % attempts', attempts;
    my_database->         RETURN;
    my_database->     END IF;
    my_database->
    my_database->     delay := interval '5 seconds' * (2 ^ attempts);
    my_database->     RAISE NOTICE 'Attempt % failed, retrying in %', attempts, delay;
    my_database->     PERFORM timetable.notify_chain_start(
    my_database->         chain_id => current_setting('pg_timetable.current_chain_id')::bigint,
    my_database->         worker_name => 'errortester',
    my_database->         start_delay => delay);
    my_database-> END
    my_database-> $$;
    ```

3. Schedule a chain that fails every time, wired to the handler with `job_on_error`:

    ```sql
    my_database=> SELECT timetable.add_job(
    my_database->     job_name          => 'flaky-job',
    my_database->     job_schedule      => '@every 1 hour',
    my_database->     job_command       => 'SELECT 1/0',
    my_database->     job_ignore_errors => FALSE,
    my_database->     job_on_error      => 'SELECT retry_with_backoff()');
     add_job
    ---------
           4
    (1 row)
    ```

    `@every 1 hour` schedules the first run immediately and every hour after — the handler's
    own retries are what make it run again sooner than that.

4. Run **pg_timetable**:

    ```bash
    # pg_timetable postgresql://scheduler:somestrong@localhost/my_database --clientname=errortester
    ```

    You'll see the task fail, then the handler's `RAISE NOTICE` messages logged roughly 5, 10,
    and 20 seconds apart, followed by a "Giving up" message on the fourth attempt:

    ```text
    2024-01-01 12:00:00.000 [ERROR] [task:1] [error:division by zero] Task execution failed
    2024-01-01 12:00:00.010 [INFO ] [notice:Attempt 0 failed, retrying in 00:00:05] [severity:NOTICE] Notice received
    2024-01-01 12:00:05.020 [INFO ] [notice:Attempt 1 failed, retrying in 00:00:10] [severity:NOTICE] Notice received
    2024-01-01 12:00:15.030 [INFO ] [notice:Attempt 2 failed, retrying in 00:00:20] [severity:NOTICE] Notice received
    2024-01-01 12:00:35.040 [INFO ] [notice:Giving up after 3 attempts] [severity:NOTICE] Notice received
    ```

5. In a second terminal, confirm every attempt was recorded:

    ```sql
    my_database=> SELECT returncode, output FROM timetable.execution_log
    my_database-> WHERE chain_id = (SELECT chain_id FROM timetable.chain WHERE chain_name = 'flaky-job')
    my_database-> ORDER BY last_run;
     returncode |          output
    ------------+---------------------------
             -1 | division by zero
             -1 | division by zero
             -1 | division by zero
             -1 | division by zero
    (4 rows)
    ```

6. PROFIT! You've handled a failing chain without babysitting it.

For every task- and chain-level error-handling field (`ignore_error`, `on_error`, `autonomous`),
see the [Commands, Tasks, and Chains Reference](reference-commands-tasks-chains.md). For a
ready-to-run version of this pattern with configurable limits, see
[`samples/DelayedRetry.sql`](https://github.com/cybertec-postgresql/pg_timetable/blob/master/samples/DelayedRetry.sql)
in [Samples](samples.md).
