-- This script demonstrates how to use pg_timetable to interact with etcd.
-- It includes tasks to write keys to etcd, read them back, and parse the results
-- into a structured format. The difference from the Etcd.sql example is that
-- it runs etcdctl commands locally. That allows to run pg_timetable on a remote
-- server without etcd installed on a PostgreSQL server.

-- Setup: A table to store the keys and values from etcd
CREATE TABLE IF NOT EXISTS etcd_test_data (
    key TEXT,
    value TEXT
);
TRUNCATE public.etcd_test_data;

-- An enhanced example consisting of three tasks:
-- 1. Write key-value pairs to etcd using CopyToProgram BUILT-IN task
-- 2. Read all keys under a specific prefix from etcd using CopyFromProgram BUILT-IN task
-- 3. Delete all keys under the prefix using a single parameterized task
DO $CHAIN$
DECLARE
    v_task_id bigint;
    v_chain_id bigint;
BEGIN
    -- Create the chain with default values executed every minute (NULL == '* * * * *' :: timetable.cron)
    INSERT INTO timetable.chain (chain_name, live, self_destruct)
    VALUES ('Sync etcd with PostgreSQL', TRUE, TRUE)
    RETURNING chain_id INTO v_chain_id;

    -- Step 1. Write key-value pairs to etcd
    -- Create the task to write keys to etcd
    INSERT INTO timetable.task (chain_id, task_order, kind, command, ignore_error)
    VALUES (v_chain_id, 1, 'BUILTIN', 'CopyToProgram', FALSE)
    RETURNING task_id INTO v_task_id;

    -- Create the parameters for the task
    INSERT INTO timetable.parameter (task_id, order_id, value)
    VALUES (v_task_id, 1, jsonb_build_object(
    'sql', $$COPY (
        SELECT encode('timetable/' || name::bytea, 'base64'), encode(setting::bytea, 'base64') FROM pg_settings
        ) TO STDOUT$$,
    'cmd', 'sh',
    'args', jsonb_build_array(
        '-c',
        $$while IFS=$'\t' read key_b64 value_b64; do
            [ -z "$key_b64" ] && echo "Skipping empty key" && continue
            [ -z "$value_b64" ] && echo "Skipping empty value" && continue
            key=$(printf '%s' "$key_b64" | base64 -di)
            value=$(printf '%s' "$value_b64" | base64 -di)
            etcdctl put "$key" "$value"
        done$$)
        )
    );


    -- Step 2. Read all keys under the prefix from etcd
    -- Create the task to read keys from etcd
    INSERT INTO timetable.task (chain_id, task_order, kind, command, ignore_error)
    VALUES (v_chain_id, 2, 'BUILTIN', 'CopyFromProgram', FALSE)
    RETURNING task_id INTO v_task_id;

    -- Create the parameters for the task
    INSERT INTO timetable.parameter (task_id, order_id, value)
    VALUES (v_task_id, 1, jsonb_build_object(
        'sql', 'COPY etcd_test_data FROM STDIN',
        'cmd', 'sh',
        'args', jsonb_build_array(
            '-c',
            $$etcdctl get --prefix timetable/ | awk 'NR%2==1{key=$0} NR%2==0{print key "\t" $0}'$$
            )
        )
    );

    -- Step 3. Delete all keys under the prefix from etcd
    -- Create the task to delete keys from etcd
    INSERT INTO timetable.task (chain_id, task_order, kind, command, ignore_error)
    VALUES (v_chain_id, 3, 'PROGRAM', 'etcdctl', FALSE)
    RETURNING task_id INTO v_task_id;

    -- Create the parameters for the task
    INSERT INTO timetable.parameter (task_id, order_id, value)
    VALUES (v_task_id, 1, jsonb_build_array('del', '--prefix', 'timetable/'));

END; $CHAIN$