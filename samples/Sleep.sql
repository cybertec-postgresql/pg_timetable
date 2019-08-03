WITH 
noop(id) AS (
    SELECT task_id FROM timetable.base_task WHERE name = 'Sleep'
),
chain_insert(chain_id) AS (
    INSERT INTO timetable.task_chain 
        (chain_id, parent_id, task_id, run_uid, database_connection, ignore_error)
    VALUES 
        (DEFAULT, NULL, (SELECT id FROM noop), NULL, NULL, TRUE)
    RETURNING chain_id
),
chain_config(id) as (
    INSERT INTO timetable.chain_execution_config VALUES 
    (
        DEFAULT, -- chain_execution_config, 
        (SELECT chain_id FROM chain_insert), -- chain_id, 
        'sleep every minute', -- chain_name, 
        NULL, -- run_at_minute, 
        NULL, -- run_at_hour, 
        NULL, -- run_at_day, 
        NULL, -- run_at_month,
        NULL, -- run_at_day_of_week, 
        1, -- max_instances, 
        TRUE, -- live, 
        FALSE, -- self_destruct,
        FALSE, -- exclusive_execution, 
        NULL -- excluded_execution_configs
    )
    RETURNING  chain_execution_config
)
INSERT INTO timetable.chain_execution_parameters 
    (chain_execution_config, chain_id, order_id, value)
VALUES (
    (SELECT id FROM chain_config),
    (SELECT chain_id FROM chain_insert),
    1,
    '5' :: jsonb)