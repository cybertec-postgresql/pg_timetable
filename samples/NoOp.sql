INSERT INTO timetable.chain  (
    chain_id, 
    task_id, 
    chain_name, 
    run_at, 
    max_instances, 
    live,
    self_destruct, 
    exclusive_execution
) VALUES (
    DEFAULT, -- chain_id, 
    timetable.add_task(kind := 'BUILTIN', command := 'NoOp', parent_id := NULL), -- task_id, 
    'execute noop every minute', -- chain_name, 
    '* * * * *', -- run_at, 
    1, -- max_instances, 
    TRUE, -- live, 
    FALSE, -- self_destruct,
	FALSE -- exclusive_execution, 
);