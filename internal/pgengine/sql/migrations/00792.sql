ALTER TABLE timetable.task ADD COLUMN live BOOLEAN NOT NULL DEFAULT TRUE;

COMMENT ON COLUMN timetable.task.live IS
    'Indication that the task is ready to run, set to FALSE to skip execution';
