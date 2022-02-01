DROP TABLE timetable.run_status;

DROP TYPE timetable.execution_status;

CREATE UNLOGGED TABLE timetable.active_chain(
    chain_id    BIGINT  NOT NULL,
    client_name TEXT    NOT NULL
);

COMMENT ON TABLE timetable.active_chain IS
    'Stores information about active chains within session';

CREATE OR REPLACE FUNCTION timetable.try_lock_client_name(worker_pid BIGINT, worker_name TEXT)
RETURNS bool AS
$CODE$
BEGIN
    IF pg_is_in_recovery() THEN
        RAISE NOTICE 'Cannot obtain lock on a replica. Please, use the primary node';
        RETURN FALSE;
    END IF;
    -- remove disconnected sessions
    DELETE
        FROM timetable.active_session
        WHERE server_pid NOT IN (
            SELECT pid
            FROM pg_catalog.pg_stat_activity
            WHERE application_name = 'pg_timetable'
        );
    DELETE 
        FROM timetable.active_chain 
        WHERE client_name NOT IN (
            SELECT client_name FROM timetable.active_session
        );
    -- check if there any active sessions with the client name but different client pid
    PERFORM 1
        FROM timetable.active_session s
        WHERE
            s.client_pid <> worker_pid
            AND s.client_name = worker_name
        LIMIT 1;
    IF FOUND THEN
        RAISE NOTICE 'Another client is already connected to server with name: %', worker_name;
        RETURN FALSE;
    END IF;
    -- insert current session information
    INSERT INTO timetable.active_session VALUES (worker_pid, worker_name, pg_backend_pid());
    RETURN TRUE;
END;
$CODE$
STRICT
LANGUAGE plpgsql;

DROP FUNCTION timetable.get_chain_running_statuses(chain_id BIGINT);

DROP FUNCTION timetable.health_check(client_name TEXT);