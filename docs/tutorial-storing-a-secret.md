# Storing a Secret

In this tutorial we'll store an encrypted password in the secret store, reference it from a
task's connection string, and watch **pg_timetable** resolve it at execution time without ever
writing the plaintext to a log.

1. Make sure you completed [Your First Scheduled Chain](tutorial-first-chain.md) — we'll reuse
   the same `scheduler` role and `my_database`.

2. Pick an encryption key and start **pg_timetable** with it:

    ```bash
    pg_timetable postgresql://scheduler:somestrong@localhost/my_database --clientname=secrettester --secret-key=mysecretkey123
    ```

    Leave it running; we'll come back to it in step 5.

3. In a second terminal, install `pgcrypto` and store a secret for the `secrettester` client.
   `pgcrypto` must be installed by a role with `CREATE` on the database — since PostgreSQL 13 it's
   a trusted extension, so `scheduler` can do it directly:

    ```sql
    CREATE EXTENSION IF NOT EXISTS pgcrypto;

    INSERT INTO timetable.secret (client_name, secret_name, value_enc)
    VALUES ('secrettester', 'my_pg_password',
            pgp_sym_encrypt('somestrong', 'mysecretkey123'));
    ```

    The encryption key here (`'mysecretkey123'`) must match `--secret-key` from step 2.

4. Create a chain with a task that connects to a database using `${secret:my_pg_password}` in
   its connection string instead of a literal password:

    ```sql
    WITH new_chain AS (
        INSERT INTO timetable.chain (chain_name, run_at, live)
        VALUES ('remote-greeting', '@reboot', TRUE)
        RETURNING chain_id
    ), new_task AS (
        INSERT INTO timetable.task (chain_id, task_order, command, database_connection)
        SELECT chain_id, 10, 'SELECT $1',
               'host=localhost port=5432 dbname=my_database user=scheduler password=${secret:my_pg_password}'
        FROM new_chain
        RETURNING task_id
    )
    INSERT INTO timetable.parameter (task_id, order_id, value)
    SELECT task_id, 1, '["hello from remote"]'::jsonb FROM new_task;
    ```

    Adjust `host`/`port` if your PostgreSQL server isn't on the default local address — a
    `database_connection` normally points at a genuinely remote host; here it loops back to the
    same server so the tutorial is self-contained.

5. Restart **pg_timetable** from step 2 (`Ctrl+C`, then run the same command again). `@reboot`
   chains run once, immediately on startup, so you'll see the task connect and succeed right away:

    ```text
    [chain:2|remote-greeting] Starting chain
    [chain:2|remote-greeting] [task:2] Starting task
    [chain:2|remote-greeting] [task:2] Switching to remote task mode
    [chain:2|remote-greeting] [task:2] Remote connection established...
    [chain:2|remote-greeting] [task:2] Task executed successfully
    [chain:2|remote-greeting] Chain executed successfully
    ```

    The chain and task numbers depend on what else you've created in `my_database` — only the
    names and the sequence of messages matter.

6. Confirm the task ran — and that the plaintext password never made it into the log:

    ```sql
    SELECT returncode, command, params FROM timetable.execution_log
    WHERE chain_id = (SELECT chain_id FROM timetable.chain WHERE chain_name = 'remote-greeting');
    ```

    ```text
     returncode |  command  |         params
    ------------+-----------+-------------------------
              0 | SELECT $1 | ["hello from remote"]
    (1 row)
    ```

    `params` holds your task's arguments, not the connection string, so there was never anything
    to mask here — but the same guarantee applies if the secret reference is inside `params`
    itself, e.g. in `samples/Mail.sql`.

7. PROFIT! You've resolved a secret at execution time without ever storing or logging it in
   plaintext.

For every secret-store table, function, and permission, see the
[Secret Store Reference](secret_store.md). For the write-only design and its trust boundary, see
[Secret Store Security Model](explanation-secret-store-security-model.md). For the
administrative steps (granting a separate secret-admin role, using secrets from YAML-authored
chains), see [Use the Secret Store](how-to-use-secret-store.md). For a fuller worked example with
a dedicated remote-log table, see
[`samples/RemoteDB.sql`](https://github.com/cybertec-postgresql/pg_timetable/blob/master/samples/RemoteDB.sql)
in [Samples](samples.md).
