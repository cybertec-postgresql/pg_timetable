CREATE OR REPLACE FUNCTION f1 ()
    RETURNS void
    AS $$
BEGIN
    RAISE notice 'hi';
END;
$$
LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION f2 ()
    RETURNS void
    AS $$
BEGIN
    RAISE notice 'hi2';
END;
$$
LANGUAGE plpgsql;

CREATE OR REPLACE PROCEDURE test_proc ()
    AS $$
BEGIN
    PERFORM
        f1 ();
    COMMIT;
    PERFORM
        f2 ();
    COMMIT;
END;
$$
LANGUAGE plpgsql;

WITH 
sql_task(id) AS (
    INSERT INTO timetable.base_task VALUES (
        DEFAULT,                     -- task_id
        'proc with transactions test',  -- name
        DEFAULT,                     -- 'SQL' :: timetable.task_kind
        'CALL test_proc()'     -- task script
    )
    RETURNING task_id
),
chain_insert(chain_id) AS (
    INSERT INTO timetable.task_chain 
        (task_id, ignore_error, autonomous)
    SELECT 
        id, TRUE, TRUE
    FROM sql_task
    RETURNING chain_id
)
INSERT INTO timetable.chain_execution_config (
    chain_execution_config, 
    chain_id, 
    chain_name, 
    run_at, 
    max_instances, 
    live,
    self_destruct, 
    exclusive_execution, 
    excluded_execution_configs
)  VALUES (
    DEFAULT, -- chain_execution_config, 
    (SELECT chain_id FROM chain_insert), -- chain_id, 
    'call proc() every 10 sec', -- chain_name, 
    '@every 10 seconds', -- run_at, 
    1, -- max_instances, 
    TRUE, -- live, 
    FALSE, -- self_destruct,
    FALSE, -- exclusive_execution, 
    NULL -- excluded_execution_configs
)
RETURNING  chain_execution_config, run_at;
