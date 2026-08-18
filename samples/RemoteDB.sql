-- RemoteDB.sql demonstrates a remote-database task whose connection string
-- references a stored secret. The demo is same-cluster (loopback) for
-- testability; the same pattern applies to genuine cross-host connections.
--
-- pg_timetable itself NEVER installs the pgcrypto extension. As a demo a
-- user runs deliberately, this sample installs pgcrypto itself and uses the
-- unqualified pgp_sym_encrypt call.
--
-- Decision: client_name is derived from
-- `pg_timetable.current_client_name` via current_setting() so the sample is
-- self-contained under TestSamplesScripts; the test harness sets the matching
-- fixed encryption key.
CREATE EXTENSION IF NOT EXISTS pgcrypto;

DO $$
DECLARE
    v_task_id bigint;
    v_chain_id bigint;
    v_database_connection bigint;
    v_client_name text;
BEGIN
    -- In order to implement remote SQL execution, we will create a table on a remote machine
    CREATE TABLE IF NOT EXISTS timetable.remote_log (
        remote_log BIGSERIAL,
        remote_event TEXT,
        timestmp TIMESTAMPTZ,
        PRIMARY KEY (remote_log));

    v_client_name := coalesce(
        nullif(current_setting('pg_timetable.current_client_name', true), ''),
        'sample_client');

    -- Store the remote DB password encrypted. pgcrypto is required for the
    -- secret store; here it lives in `public` (the default), so the call is
    -- unqualified.
    VALUES (v_client_name, 'remotedb_demo',
            pgp_sym_encrypt('somestrong', 'pgtt_test_secret_key'))
    ON CONFLICT (client_name, secret_name) DO UPDATE
        SET value_enc = EXCLUDED.value_enc;

    -- add a remote job
    INSERT INTO timetable.chain (chain_id, chain_name, run_at, live)
    VALUES (DEFAULT, 'remote_db', '* * * * *', TRUE)
    RETURNING chain_id INTO v_chain_id;

    INSERT INTO timetable.task (chain_id, task_order, command, database_connection, ignore_error)
    VALUES (v_chain_id,
            1,
            'INSERT INTO timetable.remote_log(remote_event, timestmp) VALUES ($1, CURRENT_TIMESTAMP)',
            format('host=%s port=%s dbname=%I user=%I password=${secret:remotedb_demo}',
                    inet_server_addr(),
                    inet_server_port(),
                    current_database(),
                    session_user
                    ),
            TRUE)
    RETURNING
        task_id INTO v_task_id;

    --Parameter values for task
    INSERT INTO timetable.parameter (task_id, order_id, value)
    VALUES
        (v_task_id, 1, '["Row 1 added"]'::jsonb),
        (v_task_id, 2, '["Row 2 added"]'::jsonb);
END;
$$ LANGUAGE PLPGSQL;
