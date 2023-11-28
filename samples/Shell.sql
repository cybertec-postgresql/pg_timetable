-- An example for using the PROGRAM task.
SELECT timetable.add_job(
    job_name            => 'psql chain',
    job_schedule        => '* * * * *',
    job_kind            => 'PROGRAM'::timetable.command_kind,
    job_command         => 'psql',
    job_parameters      => ('[
			"-h", "' || host(inet_server_addr()) || '",
			"-p", "' || inet_server_port() || '",
			"-d", "' || current_database() || '",
			"-U", "' || current_user || '",
			"-w", "-c", "SELECT now();"
		]')::jsonb
) as chain_id;