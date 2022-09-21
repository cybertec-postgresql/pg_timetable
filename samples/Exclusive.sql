CREATE OR REPLACE FUNCTION sleepy_func(text)
  RETURNS void LANGUAGE plpgsql AS
$BODY$ 
BEGIN 
   RAISE NOTICE 'Sleeping for 5 sec in %', $1;
   PERFORM pg_sleep_for('5 seconds');
   RAISE NOTICE 'Waking up in %', $1;
END; 
$BODY$;

SELECT timetable.add_job(
    job_name            => 'exclusive sleepy task every 10 sec',
    job_schedule        => '@every 10 seconds',
    job_command         => 'SELECT sleepy_func($1)',
    job_parameters      => '[ "Configuration EVERY 10sec" ]'::jsonb,
    job_kind            => 'SQL'::timetable.command_kind,
    job_max_instances   => 1,
    job_live            => TRUE,
    job_exclusive       => TRUE
) as chain_id
UNION
SELECT timetable.add_job(
    job_name            => 'exclusive sleepy task after 10 sec',
    job_schedule        => '@after 10 seconds',
    job_command         => 'SELECT sleepy_func($1)',
    job_parameters      => '[ "Configuration AFTER 10sec" ]'::jsonb,
    job_kind            => 'SQL'::timetable.command_kind,
    job_max_instances   => 1,
    job_live            => TRUE,
    job_exclusive       => TRUE
);
