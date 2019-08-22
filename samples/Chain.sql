DO $$

    -- In order to create a task chain, we have to create a number of base tasks,
    -- each task_id will be associated with a chain_id.
    -- There will be only one HEAD chain (parent_id = null).
    -- chain_id of HEAD chain will be parent_id of other chains.

DECLARE
    v_child_task_id     bigint;
    v_parent_task_id    bigint;
    v_parent_id         bigint;
    v_chain_id          bigint;
    v_chain_config_id   bigint;
BEGIN

    -- In order to implement chain pperation, we will create a table(One time)

    CREATE TABLE timetable.chain_log (
        chain_log BIGSERIAL,
        EVENT TEXT,
        time TIMESTAMPTZ,
        PRIMARY KEY (chain_log)
    )


    --Add a Task

    INSERT INTO timetable.base_task VALUES (
	    DEFAULT, 						                                                -- task_id
	    'insert in chain log task',	                                                                        -- name
	    DEFAULT, 						                                                -- 'SQL' :: timetable.task_kind
	    'INSERT INTO timetable.chain_log (EVENT, time) VALUES ($1, CURRENT_TIMESTAMP);'	                -- task script
	    )
    RETURNING task_id INTO v_parent_task_id;
	
    -- attach task to a chain, This chain will be HEAD chain

    INSERT INTO timetable.task_chain 
            (chain_id, parent_id, task_id, run_uid, database_connection, ignore_error)
        VALUES 
            (DEFAULT, NULL, v_parent_task_id, NULL, NULL, TRUE)
    RETURNING chain_id INTO v_parent_id;


    -- Add few nore tasks and chains, these chains will keep parent_id value which is chain_id of HEAD node
    INSERT INTO timetable.base_task VALUES (
	    DEFAULT, 						                                                    -- task_id
	    'Update Chain_log child task',				                                            -- name
	    DEFAULT, 						                                                    -- 'SQL' :: timetable.task_kind
	    'INSERT INTO timetable.chain_log (EVENT, time) VALUES ($1, CURRENT_TIMESTAMP);'		            -- task script
	    )
    RETURNING task_id into v_child_task_id;
	
    INSERT INTO timetable.task_chain 
            (chain_id, parent_id, task_id, run_uid, database_connection, ignore_error)
        VALUES 
            (                                      
            DEFAULT,                --Chain_id
            v_parent_id,            --parent_id
            v_child_task_id,        --task_id
            NULL,                   --run_uid   
            NULL,                   --database_connection
            TRUE                    --ignore_error
            )
    RETURNING chain_id INTO v_chain_id;

    INSERT INTO timetable.chain_execution_config VALUES 
        (
        DEFAULT, -- chain_execution_config, 
        v_parent_id, -- chain_id, 
        'chain operation', -- chain_name
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
    RETURNING  chain_execution_config INTO v_chain_config_id;

    --Paremeter for HEAD(Parent) Chain
    INSERT INTO timetable.chain_execution_parameters(
        chain_execution_config, 
        chain_id, 
        order_id, 
        value
        )
        VALUES (
            v_chain_config_id,
            v_parent_id,
            1,
            '["Added"]' :: jsonb
        );
  

    --Parameter for child  chains
    INSERT INTO timetable.chain_execution_parameters (
        chain_execution_config,
        chain_id, 
        order_id, 
        value
        )
        VALUES (
            v_chain_config_id,
            v_chain_id,
            1,
            '["Updated"]' :: jsonb
        );

END;
$$
LANGUAGE 'plpgsql';
