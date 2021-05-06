WITH chain_insert(task_id) AS (
    INSERT INTO timetable.task(command, kind)
    SELECT 'HelloWorld' || i, 'PROGRAM'
    FROM generate_series(1, 500) i
    RETURNING task_id
),
chain_config(id, task_id) as (
    INSERT INTO timetable.chain (
        task_id,
        chain_name,
        run_at,
        max_instances,
        live,
        self_destruct,
        exclusive_execution
    )
    SELECT
        task_id,
        'print hello via echo from ' || task_id ,
        '* * * * *',
        1,
        TRUE,
        FALSE,
        FALSE
    FROM chain_insert
    RETURNING chain_id, task_id
  )

-- 1 param to the program command
INSERT INTO timetable.parameter (chain_id, task_id, order_id, value)
SELECT id, task_id, 1, format('["hello world from %s"]', task_id) :: jsonb
FROM chain_config;
