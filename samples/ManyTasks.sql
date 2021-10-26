SELECT timetable.add_job(
    job_name            => 'HelloWorld' || g.i,
    job_schedule        => '* * * * *',
    job_kind            => 'PROGRAM'::timetable.command_kind,
    job_command         => 'bash',
    job_parameters      => jsonb_build_array('-c', 'echo Hello World from ' || g.i),
    job_live            => TRUE,
    job_ignore_errors   => TRUE
) as chain_id FROM generate_series(1, 500) AS g(i);