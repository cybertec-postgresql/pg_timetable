SELECT timetable.add_job(
    job_name            => 'notify every minute',
    job_schedule        => '* * * * *',
    job_command         => 'SELECT pg_notify($1, $2)',
    job_parameters      => '[ "TT_CHANNEL", "Ahoj from SQL base task" ]' :: jsonb,
    job_kind            => 'SQL'::timetable.command_kind,
    job_client_name     => NULL,
    job_max_instances   => 1,
    job_live            => TRUE,
    job_self_destruct   => FALSE,
    job_ignore_errors   => TRUE
) as chain_id;