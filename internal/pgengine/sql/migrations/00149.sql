CREATE UNLOGGED TABLE timetable.active_session(
	client_pid BIGINT NOT NULL,
	client_name TEXT NOT NULL,
	server_pid BIGINT NOT NULL
);

CREATE OR REPLACE FUNCTION timetable.try_lock_client_name(worker_pid BIGINT, worker_name TEXT)
RETURNS bool AS 
$CODE$
BEGIN
	-- remove disconnected sessions
	DELETE 
		FROM timetable.active_session 
		WHERE server_pid NOT IN (
			SELECT pid 
			FROM pg_catalog.pg_stat_activity 
			WHERE application_name = 'pg_timetable'
		);
	-- check if there any active sessions with the client name but different client pid
	PERFORM 1 
		FROM timetable.active_session s 
		WHERE 
			s.client_pid <> worker_pid
			AND s.client_name = worker_name
		LIMIT 1;
	IF FOUND THEN
		RETURN FALSE;
	END IF;
	-- insert current session information
	INSERT INTO timetable.active_session VALUES (worker_pid, worker_name, pg_backend_pid());
	RETURN TRUE;
END;	
$CODE$
STRICT
LANGUAGE plpgsql;