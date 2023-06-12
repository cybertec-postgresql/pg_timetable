CREATE OR REPLACE FUNCTION retry_chain_on_error(
	worker_name text, 
	maximum_retry_count integer, 
	minimum_retry_timeout interval, 
	maximum_retry_duration interval) 
RETURNS void 
LANGUAGE plpgsql
AS $$
DECLARE 
	last_retry interval;
	retries integer;
BEGIN
     SELECT now() - max(finished), count(*)  
     	INTO last_retry, retries
 		FROM timetable.execution_log
		WHERE chain_id = current_setting('pg_timetable.current_chain_id')::bigint 
		AND returncode != 0 AND client_name = worker_name
		AND finished BETWEEN now() - maximum_retry_duration AND now() - minimum_retry_timeout;

	RAISE NOTICE 'Last retry attempt made % ago; that was % retry attempt', last_retry, retries;
	IF retries >= maximum_retry_count THEN
		RAISE NOTICE 'The number % of retry attempts exceeds the max retry count %', retries, maximum_retry_count;
		RETURN;
	END IF;
	last_retry := coalesce(last_retry * 2, minimum_retry_timeout);
	RAISE NOTICE 'Schedule a retry attempt in %', last_retry;
	PERFORM timetable.notify_chain_start(
    	chain_id => current_setting('pg_timetable.current_chain_id')::bigint, 
    	worker_name => worker_name,
    	start_delay => last_retry);
END
$$;

SELECT timetable.add_job(
        job_name            => 'retry if fail',
        job_schedule        => '@every 10 minutes',
        job_command         => 'SELECT 42/0',
        job_kind            => 'SQL'::timetable.command_kind,
        job_live            => TRUE,
        job_ignore_errors   => FALSE,
        job_on_error        => $$SELECT retry_chain_on_error(
            worker_name => 'worker001', 
            maximum_retry_count => 3, 
            minimum_retry_timeout => interval '10 seconds', 
            maximum_retry_duration => interval '5 minutes')$$
    )