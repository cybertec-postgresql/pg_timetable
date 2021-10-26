CREATE OR REPLACE FUNCTION raise_func(text)
  RETURNS void LANGUAGE plpgsql AS
$BODY$ 
BEGIN 
   RAISE NOTICE '%', $1; 
END; 
$BODY$;

SELECT timetable.add_job(
    job_name            => 'raise client message every minute',
    job_schedule        => '* * * * *',
    job_command         => 'SELECT raise_func($1)',
    job_parameters      => '[ "Hey from client messages task" ]' :: jsonb,
    job_kind            => 'SQL'::timetable.command_kind,
    job_client_name     => NULL,
    job_max_instances   => 1,
    job_live            => TRUE,
    job_self_destruct   => FALSE,
    job_ignore_errors   => TRUE
) as chain_id;