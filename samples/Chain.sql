DO $$

    -- In order to create a task chain, we have to create a number of base tasks,
    -- each of which will be associated with a chain_id.
    -- There will be only one HEAD chain (parent_id = null).
    -- The HEADs chain_id will be the parent_id of all other chains.

DECLARE
    v_child_task_id     bigint;
    v_parent_task_id    bigint;
    v_parent_id         bigint;
    v_chain_id          bigint;
    v_chain_config_id   bigint;
BEGIN

    -- Create a showcase table on which we will demonstrate the chain operations.
    CREATE TABLE timetable.chain_log (
        chain_log BIGSERIAL,
        event TEXT,
        time TIMESTAMPTZ,
        PRIMARY KEY (chain_log)
    );

    -- Add a Task
    INSERT INTO timetable.base_task (name, kind, script)
        VALUES ('insert in chain log task', 'SQL', 'INSERT INTO timetable.chain_log (event, time) VALUES ($1, CURRENT_TIMESTAMP);')
        RETURNING
            task_id INTO v_parent_task_id;

    -- Attach the task to a chain (this will be our HEAD chain)
    INSERT INTO timetable.task_chain (task_id)
        VALUES (v_parent_task_id)
        RETURNING
            chain_id INTO v_parent_id;

    -- Add a few more tasks and chains, all of which will receive the chain_id of the HEAD chain as their parent_id
    INSERT INTO timetable.base_task (name, kind, script)
        VALUES ('Update Chain_log child task', 'SQL', 'INSERT INTO timetable.chain_log (event, time) VALUES ($1, CURRENT_TIMESTAMP);')
        RETURNING
            task_id INTO v_child_task_id;

    INSERT INTO timetable.task_chain (parent_id, task_id)
        VALUES (v_parent_id, v_child_task_id)
        RETURNING
            chain_id INTO v_chain_id;

    INSERT INTO timetable.chain_execution_config (chain_id, chain_name, max_instances, live)
        VALUES (v_parent_id, 'chain operation', 1, TRUE)
        RETURNING
            chain_execution_config INTO v_chain_config_id;

    -- Parameter for the HEAD chain
    INSERT INTO timetable.chain_execution_parameters (chain_execution_config, chain_id, order_id, value)
    VALUES (v_chain_config_id, v_parent_id, 1, '["Added"]'::jsonb);

    -- Parameter for the child chains
    INSERT INTO timetable.chain_execution_parameters (chain_execution_config, chain_id, order_id, value)
    VALUES (v_chain_config_id, v_chain_id, 1, '["Updated"]'::jsonb);

END;
$$
LANGUAGE 'plpgsql';
