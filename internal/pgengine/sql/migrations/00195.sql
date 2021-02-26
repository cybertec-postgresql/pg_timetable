CREATE OR REPLACE FUNCTION timetable.notify_chain_start(chain_config_id BIGINT, worker_name TEXT)
RETURNS void AS 
$$
  SELECT pg_notify(
  	worker_name, 
	format('{"ConfigID": %s, "Command": "START", "Ts": %s}', 
		chain_config_id, 
		EXTRACT(epoch FROM clock_timestamp())::bigint)
	)
$$
LANGUAGE SQL;

CREATE OR REPLACE FUNCTION timetable.notify_chain_stop(chain_config_id BIGINT, worker_name TEXT)
RETURNS void AS 
$$
  SELECT pg_notify(
  	worker_name, 
	format('{"ConfigID": %s, "Command": "STOP", "Ts": %s}', 
		chain_config_id, 
		EXTRACT(epoch FROM clock_timestamp())::bigint)
	)
$$
LANGUAGE SQL;