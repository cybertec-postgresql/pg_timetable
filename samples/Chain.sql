DO $$

    -- In order to create chain of tasks, We will create few base tasks and 
    -- each command_id will be associated with a task_id.
    -- There will be only one HEAD chain (parent_id = null).
    -- task_id of HEAD chain will be parent_id of other chains.

DECLARE
    v_child_command_id     bigint;
    v_parent_command_id    bigint;
    v_parent_id         bigint;
    v_task_id          bigint;
    v_chain_config_id   bigint;
BEGIN

    -- In order to implement chain pperation, we will create a table(One time)

    CREATE TABLE timetable.chain_log (
        chain_log BIGSERIAL,
        EVENT TEXT,
        time TIMESTAMPTZ,
        PRIMARY KEY (chain_log)
    );


    --Add a Task

    INSERT INTO timetable.command VALUES (
	    DEFAULT, 						                                                -- command_id
	    'insert in chain log task',	                                                    -- name
	    DEFAULT, 						                                                -- 'SQL' :: timetable.command_kind
	    'INSERT INTO timetable.chain_log (EVENT, time) VALUES ($1, CURRENT_TIMESTAMP);'	-- task script
	    )
    RETURNING command_id INTO v_parent_command_id;
	
    -- attach task to a chain, This chain will be HEAD chain

    INSERT INTO timetable.task 
            (task_id, parent_id, command_id, run_as, database_connection, ignore_error)
        VALUES 
            (DEFAULT, NULL, v_parent_command_id, NULL, NULL, TRUE)
    RETURNING task_id INTO v_parent_id;


    -- Add few nore tasks and chains, these chains will keep parent_id value which is task_id of HEAD node
    INSERT INTO timetable.command VALUES (
	    DEFAULT, 						                                                    -- command_id
	    'Update Chain_log child task',				                                        -- name
	    DEFAULT, 						                                                    -- 'SQL' :: timetable.command_kind
	    'INSERT INTO timetable.chain_log (EVENT, time) VALUES ($1, CURRENT_TIMESTAMP);'		-- task script
	    )
    RETURNING command_id into v_child_command_id;
	
    INSERT INTO timetable.task 
            (task_id, parent_id, command_id, run_as, database_connection, ignore_error)
        VALUES 
            (                                      
            DEFAULT,                --task_id
            v_parent_id,            --parent_id
            v_child_command_id,        --command_id
            NULL,                   --run_as   
            NULL,                   --database_connection
            TRUE                    --ignore_error
            )
    RETURNING task_id INTO v_task_id;

    INSERT INTO timetable.chain (
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
        v_parent_id, -- task_id, 
        'chain operation', -- chain_name
        '* * * * *', -- run_at, 
        1, -- max_instances, 
        TRUE, -- live, 
        FALSE, -- self_destruct,
        FALSE -- exclusive_execution, 
        )
    RETURNING  chain_id INTO v_chain_config_id;

    --Paremeter for HEAD(Parent) Chain
    INSERT INTO timetable.parameter(
        chain_id, 
        task_id, 
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
    INSERT INTO timetable.parameter (
        chain_id,
        task_id, 
        order_id, 
        value
        )
        VALUES (
            v_chain_config_id,
            v_task_id,
            1,
            '["Updated"]' :: jsonb
        );

END;
$$
LANGUAGE 'plpgsql';