DO $$
DECLARE 
    v_task_id bigint;
    v_chain_id bigint;
    v_database_connection bigint;
BEGIN
    -- In order to implement remote SQL execution, we will create a table on a remote machine
    CREATE TABLE IF NOT EXISTS timetable.remote_log (
        remote_log BIGSERIAL,
        remote_event TEXT,
        timestmp TIMESTAMPTZ,
        PRIMARY KEY (remote_log));

    -- add a remote job
    INSERT INTO timetable.chain (chain_id, chain_name, run_at, live) 
    VALUES (DEFAULT, 'remote db', '* * * * *', TRUE)
    RETURNING chain_id INTO v_chain_id;

    INSERT INTO timetable.task (chain_id, task_order, command, database_connection, ignore_error)
    VALUES (v_chain_id, 
            1,
            'INSERT INTO timetable.remote_log(remote_event, timestmp) VALUES ($1, CURRENT_TIMESTAMP)', 
            format('host=%s port=%s dbname=%I user=%I password=somestrong', 
                    inet_server_addr(), 
                    inet_server_port(),
                    current_database(),
                    session_user
                    ), 
            TRUE)
    RETURNING
        task_id INTO v_task_id;

    --Parameter values for task
    INSERT INTO timetable.parameter (task_id, order_id, value)
    VALUES 
        (v_task_id, 1, '["Row 1 added"]'::jsonb), 
        (v_task_id, 2, '["Row 2 added"]'::jsonb);
END;
$$ LANGUAGE PLPGSQL;
