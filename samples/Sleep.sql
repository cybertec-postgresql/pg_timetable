WITH 
chain_insert(task_id) AS (
    INSERT INTO timetable.task 
        (kind, command, ignore_error)
    VALUES 
        ('BUILTIN', 'Sleep', TRUE)
    RETURNING task_id
),
chain_config(id) as (
    INSERT INTO timetable.chain (
        chain_id, 
        task_id, 
        chain_name, 
        run_at, 
        max_instances, 
        live
    ) VALUES ( 
        DEFAULT, -- chain_id, 
        (SELECT task_id FROM chain_insert), -- task_id, 
        'sleep every minute', -- chain_name, 
        '* * * * *', -- run_at, 
        1, -- max_instances, 
        TRUE
    )
    RETURNING  chain_id
)
INSERT INTO timetable.parameter 
    (chain_id, task_id, order_id, value)
VALUES (
    (SELECT id FROM chain_config),
    (SELECT task_id FROM chain_insert),
    1,
    '5' :: jsonb)