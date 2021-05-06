DO $$
DECLARE 
    v_task_id bigint;
    v_chain_config_id bigint;
    v_database_connection bigint;
BEGIN

    -- In order to implement remote SQL execution, we will create a table on a remote machine
    CREATE TABLE timetable.remote_log (
        remote_log BIGSERIAL,
        remote_event TEXT,
        timestmp TIMESTAMPTZ,
        PRIMARY KEY (remote_log));

    -- add a Task
    INSERT INTO timetable.task (command, database_connection, ignore_error)
    VALUES ('INSERT INTO timetable.remote_log (remote_event, timestmp) VALUES ($1, CURRENT_TIMESTAMP)', 
            format('host=%s port=%s dbname=%I user=%I password=somestrong', 
                    inet_server_addr(), 
                    inet_server_port(),
                    current_database(),
                    session_user
                    ), TRUE)
    RETURNING
        task_id INTO v_task_id;

    --chain configuration
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
        v_task_id, -- task_id,
        'remote db', -- chain_name
        '* * * * *', -- run_at,
        1, -- max_instances,
        TRUE, -- live,
        FALSE, -- self_destruct,
        FALSE -- exclusive_execution,
    )
    RETURNING
        chain_id INTO v_chain_config_id;

    --Paremeter for task
    INSERT INTO timetable.parameter (chain_id, task_id, order_id, value)
        VALUES (v_chain_config_id, v_task_id, 1, '["Added"]'::jsonb);
END;
$$ LANGUAGE PLPGSQL;