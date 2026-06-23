CREATE OR REPLACE FUNCTION raise_notice_func(text)
  RETURNS void LANGUAGE plpgsql AS
$BODY$ 
BEGIN 
   RAISE NOTICE '%', $1; 
END; 
$BODY$;

DELETE FROM timetable.chain WHERE chain_name = 'notify_then_destruct';

SELECT timetable.add_job(
    job_name            => 'notify_then_destruct',
    job_schedule        => '* * * * *',
    job_command         => 'SELECT raise_notice_func($1)',
    job_parameters      => '[ "Ahoj from self destruct task" ]' :: jsonb,
    job_kind            => 'SQL'::timetable.command_kind,
    job_live            => TRUE,
    job_self_destruct   => TRUE
) as chain_id;