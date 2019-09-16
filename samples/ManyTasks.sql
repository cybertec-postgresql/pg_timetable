WITH
   hello_task(id) AS (
        insert into base_task (name, kind, script)
            select 'HelloWorld'||i, 'SHELL', 'echo'
            from generate_series(1,500) i
            returning task_id
    ),

    chain_insert(chain_id) AS (
        INSERT INTO timetable.task_chain
            (parent_id, task_id, run_uid, database_connection, ignore_error)
            select
              NULL, id, NULL, NULL, FALSE
            from hello_task
            RETURNING chain_id
    ),

    chain_config(id, chain_id) as (
        INSERT INTO timetable.chain_execution_config (
              chain_id,
              chain_name,
              run_at_minute,
              run_at_hour,
              run_at_day,
              run_at_month,
              run_at_day_of_week,
              max_instances,
              live,
              self_destruct,
              exclusive_execution,
              excluded_execution_configs
            )
            select
              chain_id, -- chain_id,
              'print hello via echo from ' || chain_id , -- chain_name,
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
              FROM chain_insert
            RETURNING  chain_execution_config, chain_id
    )

-- 1 param to the shell command
INSERT INTO timetable.chain_execution_parameters
(chain_execution_config, chain_id, order_id, value)
            SELECT
                   id,
                   chain_id,
                    1,
                    format('["hello world from %s"]', chain_id) :: jsonb
            FROM chain_config;