INSERT INTO timetable.base_task(task_id, name, script, kind) VALUES
	(DEFAULT, 'NoOp', 'NoOp', 'BUILTIN'),
	(DEFAULT, 'Sleep', 'Sleep', 'BUILTIN'),
	(DEFAULT, 'Log', 'Log', 'BUILTIN'),
	(DEFAULT, 'SendMail', 'SendMail', 'BUILTIN'),
	(DEFAULT, 'Download', 'Download', 'BUILTIN');

CREATE OR REPLACE FUNCTION timetable.get_task_id(task_name TEXT) 
RETURNS BIGINT AS $$
	SELECT task_id FROM timetable.base_task WHERE name = $1;
$$ LANGUAGE 'sql'
STRICT;
