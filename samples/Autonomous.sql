-- An advanced example showing how to use atutonomous tasks.
-- This one-task chain will execute test_proc() procedure.
-- Since procedure will make two commits (after f1() and f2())
-- we cannot use it as a regular task, because all regular tasks 
-- must be executed in the context of a single chain transaction.
-- Same rule applies for some other SQL commands, 
-- e.g. CREATE DATABASE, REINDEX, VACUUM, CREATE TABLESPACE, etc.
CREATE OR REPLACE FUNCTION f (msg TEXT) RETURNS void AS $$
BEGIN 
    RAISE notice '%', msg; 
END;
$$ LANGUAGE PLPGSQL;

CREATE OR REPLACE PROCEDURE test_proc () AS $$
BEGIN
    PERFORM f('hey 1');
    COMMIT;
    PERFORM f('hey 2');
    COMMIT;
END;
$$
LANGUAGE PLPGSQL;

WITH 
sql_task(id) AS (
    INSERT INTO timetable.command(command_id, name, kind, script) VALUES (
        DEFAULT,
        'proc with transactions test',
        'SQL' :: timetable.command_kind,
        'CALL test_proc()'
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
