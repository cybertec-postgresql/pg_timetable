ALTER TABLE timetable.execution_log
	ADD COLUMN client_name TEXT NOT NULL DEFAULT '<unknown>';
ALTER TABLE timetable.run_status
	ADD COLUMN client_name TEXT NOT NULL DEFAULT '<unknown>';
ALTER TABLE timetable.execution_log
	ALTER COLUMN client_name DROP DEFAULT;
ALTER TABLE timetable.run_status
	ALTER COLUMN client_name DROP DEFAULT;