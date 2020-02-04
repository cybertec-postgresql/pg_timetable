DO $$

DECLARE 
    v_task_id bigint;
    v_chain_id bigint;
    v_chain_config_id bigint;
    v_database_connection bigint;
BEGIN

    -- In order to implement remote SQL execution, we will create a table(One time) on Remote machine
    CREATE TABLE timetable.remote_log (
        remote_log BIGSERIAL,
        remote_event TEXT,
        timestmp TIMESTAMPTZ,
        PRIMARY KEY (remote_log));

    --Add a Task
    INSERT INTO timetable.base_task
    VALUES (DEFAULT, -- task_id
        'insert in remote log task', -- name
        DEFAULT, -- 'SQL' :: timetable.task_kind
        'INSERT INTO timetable.remote_log (remote_event, timestmp) VALUES ($1, CURRENT_TIMESTAMP);' -- task script
    )
    RETURNING
        task_id INTO v_task_id;

	--remote DB details
    INSERT INTO timetable.database_connection (database_connection, connect_string, comment)
    VALUES (DEFAULT,
            format('host=%s port=%s dbname=%I user=%I password=strongone', 
                    inet_server_addr(), 
                    inet_server_port(),
                    current_database(),
                    session_user
                    ),
            current_database() || '@' || inet_server_addr())
    RETURNING
        database_connection INTO v_database_connection;


    -- attach task to a chain
    INSERT INTO timetable.task_chain (chain_id, parent_id, task_id, run_uid, database_connection, ignore_error)
    VALUES (DEFAULT, NULL, v_task_id, NULL, v_database_connection, TRUE)
    RETURNING
        chain_id INTO v_chain_id;

    --chain configuration
    INSERT INTO timetable.chain_execution_config
    VALUES (DEFAULT, -- chain_execution_config,
        v_chain_id, -- chain_id,
        'remote db', -- chain_name
        '* * * * *', -- run_at,
        1, -- max_instances,
        TRUE, -- live,
        FALSE, -- self_destruct,
        FALSE, -- exclusive_execution,
        NULL -- excluded_execution_configs
    )
    RETURNING
        chain_execution_config INTO v_chain_config_id;

    --Paremeter for task
    INSERT INTO timetable.chain_execution_parameters (chain_execution_config, chain_id, order_id, value)
        VALUES (v_chain_config_id, v_chain_id, 1, '["Added"]'::jsonb);
 
END;
$$
LANGUAGE 'plpgsql';