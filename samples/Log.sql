SELECT timetable.add_job(
    job_name            => 'Builtin-in Log',
    job_schedule        => NULL, -- same as '* * * * *'
    job_command         => 'Log',
    job_kind            => 'BUILTIN'::timetable.command_kind,
    job_parameters      => '{"Description":"Log Execution"}'::jsonb
) as chain_id;