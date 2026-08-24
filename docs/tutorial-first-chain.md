# Your First Scheduled Chain

In this tutorial we'll install pg_timetable, schedule a one-off job, and watch it run.

1. Download pg_timetable executable.
2. Make sure your PostgreSQL server is up and running and has a role with `CREATE` privilege
   for a target database, e.g.

    ```sql
    CREATE ROLE scheduler PASSWORD 'somestrong';
    GRANT CREATE ON DATABASE my_database TO scheduler;
    ```

    This uses the simplest path — a downloaded binary. For Docker or building from source, see [Installation](installation.md).

3. Create a new job, e.g. run `VACUUM` each night at 00:30 Postgres server time zone

    ```sql
    SELECT timetable.add_job('frequent-vacuum', '30 0 * * *', 'VACUUM');
    ```

    ```text
    add_job
    ---------
          3
    (1 row)
    ```

4. Run the **pg_timetable**

    ```bash
    pg_timetable postgresql://scheduler:somestrong@localhost/my_database --clientname=vacuumer
    ```

5. PROFIT!
