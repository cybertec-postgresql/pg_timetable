DROP FUNCTION IF EXISTS timetable.notify_chain_start;
CREATE OR REPLACE FUNCTION timetable.notify_chain_start(
    chain_id    BIGINT, 
    worker_name TEXT,
    start_delay INTERVAL DEFAULT NULL
) RETURNS void AS $$
    SELECT pg_notify(
        worker_name, 
        format('{"ConfigID": %s, "Command": "START", "Ts": %s, "Delay": %s}', 
            chain_id, 
            EXTRACT(epoch FROM clock_timestamp())::bigint,
            COALESCE(EXTRACT(epoch FROM start_delay)::bigint, 0)
        )
    )
$$ LANGUAGE SQL;