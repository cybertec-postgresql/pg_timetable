-- This sample demonstrates how to read multiple key-value pairs from an etcd cluster
-- based on a prefix and reflect them in a database table.
--
-- It assumes that:
-- 1. etcdctl is installed and available in the system's PATH.
-- 2. The etcd cluster is accessible. The default endpoint is http://127.0.0.1:2379.
-- 3. The user running pg_timetable has sufficient permissions to execute the etcdctl command.

-- The chain will perform the following steps:
-- 1. Define one task to write to etcd and provide multiple parameter rows to it.
--    The task will be executed for each parameter row.
-- 2. Read all key-values under that prefix from etcd as a single JSON object.
-- 3. Parse the JSON, decode the base64 keys and values, and store them in a table.
-- 4. Log the number of keys retrieved.
-- 5. Delete all keys under the prefix from etcd using a single parameterized task.

-- Setup: A table to store the keys and values from etcd
CREATE TABLE IF NOT EXISTS etcd_test_data (
    key TEXT,
    value TEXT
);
TRUNCATE public.etcd_test_data;

-- Setup: A temporary table to store raw JSON output from etcd
CREATE TABLE IF NOT EXISTS etcd_json_raw (
    data JSONB
);
TRUNCATE etcd_json_raw;

-- The chain definition
-- It runs once and then removes itself (self_destruct).
INSERT INTO timetable.chain (
    chain_name,
    run_at,
    max_instances,
    live,
    self_destruct
)
VALUES (
    'ETCD Prefix-Read-Clean',
    '* * * * *', -- Run every minute, but self_destruct will make it run once
    1,
    TRUE,
    TRUE
)
RETURNING chain_id;

-- Task 1: A single task to write key-value pairs to etcd.
-- This task will be executed 3 times, once for each parameter row below.
WITH task AS (
    INSERT INTO timetable.task (chain_id, task_order, task_name, kind, command, ignore_error)
    SELECT currval('timetable.chain_chain_id_seq'), 10, 'Write keys to etcd', 'PROGRAM', 'etcdctl', FALSE
    RETURNING task_id
)
INSERT INTO timetable.parameter (task_id, order_id, value)
SELECT task_id, 1, '["--endpoints=http://127.0.0.1:2379", "put", "/pg_timetable/multi/key1", "value1"]'::jsonb FROM task UNION ALL
SELECT task_id, 2, '["--endpoints=http://127.0.0.1:2379", "put", "/pg_timetable/multi/key2", "value2"]'::jsonb FROM task UNION ALL
SELECT task_id, 3, '["--endpoints=http://127.0.0.1:2379", "put", "/pg_timetable/multi/key3", "value3"]'::jsonb FROM task;

-- Task 2: Read all keys under the prefix from etcd as JSON
INSERT INTO timetable.task (
    chain_id,
    task_order,
    task_name,
    kind,
    command,
    ignore_error
)
SELECT
    currval('timetable.chain_chain_id_seq'),
    20,
    'Read from etcd with prefix',
    'SQL',
    $$COPY etcd_json_raw (data) FROM PROGRAM 'etcdctl --endpoints=http://127.0.0.1:2379 get --prefix /pg_timetable/multi/ --write-out=json'$$,
    FALSE;

-- Task 3: Parse JSON array and store the key-value pairs
INSERT INTO timetable.task (
    chain_id,
    task_order,
    task_name,
    kind,
    command,
    ignore_error
)
SELECT
    currval('timetable.chain_chain_id_seq'),
    30,
    'Parse etcd output and store',
    'SQL',
    $$INSERT INTO public.etcd_test_data (key, value)
      SELECT
          convert_from(decode(kv->>'key', 'base64'), 'UTF8'),
          convert_from(decode(kv->>'value', 'base64'), 'UTF8')
      FROM
          etcd_json_raw,
          jsonb_array_elements(data->'kvs') AS kv$$,
    FALSE;

-- Task 4: Log the result
INSERT INTO timetable.task (
    chain_id,
    task_order,
    task_name,
    kind,
    command,
    ignore_error
)
SELECT
    currval('timetable.chain_chain_id_seq'),
    40,
    'Log etcd output',
    'SQL',
    $task$
    DO
        $$
            DECLARE
            msg integer;
            BEGIN
                SELECT count(*) FROM public.etcd_test_data INTO msg;
                RAISE notice 'Loaded keys from etcd: %', msg; 
            END;
        $$
    $task$,
    FALSE;

-- Task 5: Clean up the keys in etcd
WITH task AS (
    INSERT INTO timetable.task (chain_id, task_order, task_name, kind, command, ignore_error)
    SELECT currval('timetable.chain_chain_id_seq'), 50, 'Clean up etcd', 'PROGRAM', 'etcdctl', FALSE
    RETURNING task_id
)
INSERT INTO timetable.parameter (task_id, order_id, value)
SELECT task_id, 1, '["--endpoints=http://127.0.0.1:2379", "del", "--prefix", "/pg_timetable/multi/"]'::jsonb FROM task;
