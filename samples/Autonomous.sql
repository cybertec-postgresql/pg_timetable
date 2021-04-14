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
    INSERT INTO timetable.command VALUES (
        DEFAULT,                     -- command_id
        'proc with transactions test',  -- name
        DEFAULT,                     -- 'SQL' :: timetable.command_kind
        'CALL test_proc()'     -- task script
    )
    RETURNING command_id
),
chain_insert(task_id) AS (
    INSERT INTO timetable.task 
        (command_id, ignore_error, autonomous)
    SELECT 
        id, TRUE, TRUE
    FROM sql_task
    RETURNING task_id
)
INSERT INTO timetable.chain (
    chain_id, 
    task_id, 
    chain_name, 
    run_at, 
    max_instances, 
    live,
    self_destruct, 
    exclusive_execution
)  VALUES (
    DEFAULT, -- chain_id, 
    (SELECT task_id FROM chain_insert), -- task_id, 
    'call proc() every 10 sec', -- chain_name, 
    '@every 10 seconds', -- run_at, 
    1, -- max_instances, 
    TRUE, -- live, 
    FALSE, -- self_destruct,
    FALSE -- exclusive_execution, 
)
RETURNING  chain_id, run_at;
