-- 00876 Reject unknown BUILTIN command names
ALTER TABLE timetable.task
    ADD CONSTRAINT task_builtin_command_known
    CHECK (kind <> 'BUILTIN' OR command IN ('NoOp', 'Sleep', 'Log', 'SendMail', 'Download',
        'CopyFromFile', 'CopyToFile', 'CopyToProgram', 'CopyFromProgram', 'Shutdown'));
