CREATE OR REPLACE FUNCTION raise_func(text)
  RETURNS void LANGUAGE plpgsql AS
$BODY$ 
BEGIN 
   RAISE NOTICE '%', $1; 
END; 
$BODY$;

SELECT timetable.add_job(
    job_name            => 'notify then destruct',
    job_schedule        => '* * * * *',
    job_command         => 'SELECT raise_func($1)',
    job_parameters      => '[ "Ahoj from self destruct task" ]' :: jsonb,
    job_kind            => 'SQL'::timetable.command_kind,
    job_live            => TRUE,
    job_self_destruct   => TRUE
) as chain_id;