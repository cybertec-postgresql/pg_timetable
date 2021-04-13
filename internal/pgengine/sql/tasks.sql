INSERT INTO timetable.command(command_id, name, script, kind) VALUES
	(DEFAULT, 'NoOp', 'NoOp', 'BUILTIN'),
	(DEFAULT, 'Sleep', 'Sleep', 'BUILTIN'),
	(DEFAULT, 'Log', 'Log', 'BUILTIN'),
	(DEFAULT, 'SendMail', 'SendMail', 'BUILTIN'),
	(DEFAULT, 'Download', 'Download', 'BUILTIN'),
	(DEFAULT, 'CopyFromFile', 'CopyFromFile', 'BUILTIN');

CREATE OR REPLACE FUNCTION timetable.get_command_id(command_name TEXT) 
RETURNS BIGINT AS $$
	SELECT command_id FROM timetable.command WHERE name = $1;
$$ LANGUAGE 'sql'
STRICT;
