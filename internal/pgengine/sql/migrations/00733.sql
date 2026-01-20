-- Add params column to execution_log table to store parameter values used during task execution
ALTER TABLE timetable.execution_log ADD COLUMN params TEXT;

COMMENT ON COLUMN timetable.execution_log.params IS 'Array of parameter values used during task execution';
