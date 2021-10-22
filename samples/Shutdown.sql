-- This one-task chain (aka job) will terminate pg_timetable session.
-- This is useful for maintaining purposes or before database being destroyed.
-- One should take care of restarting pg_timetable if needed.

SELECT timetable.add_job (
    job_name     => 'Shutdown pg_timetable session on schedule',
    job_schedule => '* * 1 * *',
    job_command  => 'Shutdown',
    job_kind     => 'BUILTIN'
);