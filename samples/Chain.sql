DO $$

    -- In order to create chain of tasks, We will create few base tasks and 
    -- each command_id will be associated with a task_id.
    -- There will be only one HEAD chain (parent_id = null).
    -- task_id of HEAD chain will be parent_id of other chains.

DECLARE
    v_parent_id         bigint;
    v_task_id          bigint;
    v_chain_id   bigint;
BEGIN
    -- In order to implement chain pperation, we will create a table
    CREATE TABLE IF NOT EXISTS timetable.chain_log (
        chain_log BIGSERIAL,
        EVENT TEXT,
        time TIMESTAMPTZ,
        PRIMARY KEY (chain_log)
    );

    -- Let's create a new chain and add tasks to it later
    INSERT INTO timetable.chain (
        chain_id, 
        chain_name, 
        run_at, 
        max_instances, 
        live,
        self_destruct, 
        exclusive_execution
    ) VALUES (
        DEFAULT,            -- chain_id, 
        'chain operation',  -- chain_name
        '* * * * *',        -- run_at, 
        1,                  -- max_instances, 
        TRUE,               -- live, 
        FALSE,              -- self_destruct,
        FALSE               -- exclusive_execution, 
    ) RETURNING chain_id INTO v_chain_id;

    --Add a head task
    INSERT INTO timetable.task (chain_id, task_order, command, ignore_error)
    VALUES (v_chain_id, 1, 'INSERT INTO timetable.chain_log (EVENT, time) VALUES ($1, CURRENT_TIMESTAMP)', TRUE)
    RETURNING task_id INTO v_parent_id;

    -- Add one more task, this task will keep parent_id value which is task_id of the HEAD task
    INSERT INTO timetable.task (chain_id, task_order, command, ignore_error)
    VALUES (v_chain_id, 2, 'INSERT INTO timetable.chain_log (EVENT, time) VALUES ($1, CURRENT_TIMESTAMP)', TRUE)
    RETURNING task_id INTO v_task_id;

    INSERT INTO timetable.parameter(task_id, order_id, value)
    VALUES 
        -- Parameter for HEAD (parent) task
        (v_parent_id, 1, '["Added"]' :: jsonb),
        -- Parameter for the next task
        (v_task_id, 1, '["Updated"]' :: jsonb);

    -- Add one more task swowing IDs for all tasks within the chain
    INSERT INTO timetable.task (chain_id, task_order, command, ignore_error)
    VALUES (v_chain_id, 3, 
    $CMD$
        DO $BODY$ 
        DECLARE tasks TEXT;
        BEGIN 
            SELECT array_agg(task_id ORDER BY task_order) FROM timetable.task
            INTO tasks
            WHERE chain_id = current_setting('pg_timetable.current_chain_id')::bigint;
            RAISE NOTICE 'Task IDs in chain: %', tasks;
        END; 
        $BODY$
    $CMD$, TRUE);
   
END;
$$ LANGUAGE plpgsql;