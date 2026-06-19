SELECT timetable.add_job(
    job_name            => 'builtin_log_every_minute',
    job_schedule        => NULL, -- same as '* * * * *'
    job_command         => 'Log',
    job_kind            => 'BUILTIN'::timetable.command_kind,
    job_parameters      => '{"Description":"Log Execution"}'::jsonb
) as chain_id;