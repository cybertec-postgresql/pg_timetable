SELECT timetable.add_job(
    job_name            => COALESCE(jobname, 'job: ' || command),
    job_schedule        => schedule,
    job_command         => command,
    job_kind            => 'SQL',
    job_live            => active
) FROM cron.job;