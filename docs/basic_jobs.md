# Getting started

A variety of examples can be found in the [samples](samples.md). If you want to migrate from a different scheduler, you can use scripts from [migration](migration.md) chapter.

## Add simple job

In a real world usually it's enough to use simple jobs. Under this term we understand:

* job is a chain with only one **task** (step) in it;
* it doesn't use complicated logic, but rather simple **command**;
* it doesn't require complex transaction handling, since one task is implicitely executed as a single transaction.

For such a group of chains we've introduced a special function `timetable.add_job()`.

### Function: `timetable.add_job()`

Creates a simple one-task chain

**Returns:** `BIGINT`

#### Parameters

| Parameter | Type | Description | Default |
|-----------|------|-------------|---------|
| `job_name` | `text` | The unique name of the **chain** and **command** | Required |
| `job_schedule` | `timetable.cron` | Time schedule in сron syntax at Postgres server time zone | Required |
| `job_command` | `text` | The SQL which will be executed | Required |
| `job_parameters` | `jsonb` | Arguments for the chain **command** | `NULL` |
| `job_kind` | `timetable.command_kind` | Kind of the command: *SQL*, *PROGRAM* or *BUILTIN* | `SQL` |
| `job_client_name` | `text` | Specifies which client should execute the chain. Set this to `NULL` to allow any client | `NULL` |
| `job_max_instances` | `integer` | The amount of instances that this chain may have running at the same time | `NULL` |
| `job_live` | `boolean` | Control if the chain may be executed once it reaches its schedule | `TRUE` |
| `job_self_destruct` | `boolean` | Self destruct the chain after execution | `FALSE` |
| `job_ignore_errors` | `boolean` | Ignore error during execution | `TRUE` |
| `job_exclusive` | `boolean` | Execute the chain in the exclusive mode | `FALSE` |

**Returns:** the ID of the created chain

## Examples

1. Run `public.my_func()` at 00:05 every day in August Postgres server time zone:

    ```sql
    SELECT timetable.add_job('execute-func', '5 0 * 8 *', 'SELECT public.my_func()');
    ```

2. Run `VACUUM` at minute 23 past every 2nd hour from 0 through 20 every day Postgres server time zone:

    ```sql
    SELECT timetable.add_job('run-vacuum', '23 0-20/2 * * *', 'VACUUM');
    ```

3. Refresh materialized view every 2 hours:

    ```sql
    SELECT timetable.add_job('refresh-matview', '@every 2 hours', 'REFRESH MATERIALIZED VIEW public.mat_view');
    ```

4. Clear log table after **pg_timetable** restart:

    ```sql
    SELECT timetable.add_job('clear-log', '@reboot', 'TRUNCATE timetable.log');
    ```

5. Reindex at midnight Postgres server time zone on Sundays with [reindexdb](https://www.postgresql.org/docs/current/app-reindexdb.html) utility:

    - using default database under default user (no command line arguments)
  
        ```sql
        SELECT timetable.add_job('reindex', '0 0 * * 7', 'reindexdb', job_kind := 'PROGRAM');
        ```
    
    - specifying target database and tables, and be verbose

        ```sql
        SELECT timetable.add_job('reindex', '0 0 * * 7', 'reindexdb', 
            '["--table=foo", "--dbname=postgres", "--verbose"]'::jsonb, 'PROGRAM');
        ```

    - passing password using environment variable through `bash` shell

        ```sql
        SELECT timetable.add_job('reindex', '0 0 * * 7', 'bash', 
            '["-c", "PGPASSWORD=5m3R7K4754p4m reindexdb -U postgres -h 192.168.0.221 -v"]'::jsonb, 
            'PROGRAM');
        ```