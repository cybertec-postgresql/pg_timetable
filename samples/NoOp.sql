SELECT timetable.add_job(
    job_name            => 'noop_every_minute',
    job_schedule        => '* * * * *',
    job_command         => 'NoOp',
    job_kind            => 'BUILTIN'::timetable.command_kind
) as chain_id;