WITH
  hello_task(id) AS (
    INSERT INTO timetable.command (name, kind, script)
        SELECT 'HelloWorld'||i, 'PROGRAM', 'echo'
        FROM generate_series(1,500) i
        returning command_id
  ),

  chain_insert(task_id) AS (
    INSERT INTO timetable.task
        (parent_id, command_id, run_as, database_connection, ignore_error)
        select
          NULL, id, NULL, NULL, FALSE
        from hello_task
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
      task_id, -- task_id,
      'print hello via echo from ' || task_id , -- chain_name,
      '* * * * *', -- run_at,
      1, -- max_instances,
      TRUE, -- live,
      FALSE, -- self_destruct,
      FALSE -- exclusive_execution,
    FROM chain_insert
    RETURNING  chain_id, task_id
  )

-- 1 param to the program command
INSERT INTO timetable.parameter (chain_id, task_id, order_id, value)
SELECT id, task_id, 1, format('["hello world from %s"]', task_id) :: jsonb
FROM chain_config;
