SELECT timetable.add_job(
    job_name            => 'sleep every minute',
    job_schedule        => '@every 10 seconds',
    job_command         => 'Sleep',
    job_parameters      => '20' :: jsonb,
    job_kind            => 'BUILTIN'::timetable.command_kind,
    job_client_name     => NULL,
    job_max_instances   => 1,
    job_live            => TRUE
) as chain_id;