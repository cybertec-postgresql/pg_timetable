SELECT timetable.add_job(
        job_name            => 'fail',
        job_schedule        => '* * * * *',
        job_command         => 'SELECT 42/0',
        job_kind            => 'SQL'::timetable.command_kind,
        job_live            => TRUE,
        job_ignore_errors   => FALSE,
        job_on_error        => $$SELECT pg_notify('monitoring', 
            format('{"ConfigID": %s, "Message": "Something bad happened"}', 
                current_setting('pg_timetable.current_chain_id')::bigint))$$
    )