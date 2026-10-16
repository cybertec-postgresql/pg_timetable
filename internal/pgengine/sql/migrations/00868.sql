-- 00868 Check parameter value shape
CREATE OR REPLACE FUNCTION timetable.parameter_shape_error(kind timetable.command_kind, command TEXT, value JSONB)
RETURNS TEXT AS
$CODE$
    SELECT CASE
        WHEN value IS NULL OR jsonb_typeof(value) = 'null' THEN NULL
        WHEN kind = 'SQL' THEN
            CASE WHEN jsonb_typeof(value) <> 'array' THEN 'a JSON array' END
        WHEN kind = 'PROGRAM' THEN
            CASE WHEN jsonb_typeof(value) <> 'array'
                OR jsonb_path_exists(value, '$[*] ? (@.type() != "string")') THEN 'a JSON array of strings' END
        WHEN command = 'Sleep' THEN
            CASE WHEN jsonb_typeof(value) <> 'number' OR value::text !~ '^-?[0-9]+$' THEN 'an integer' END
        WHEN command IN ('SendMail', 'Download', 'CopyFromFile', 'CopyToFile', 'CopyToProgram', 'CopyFromProgram') THEN
            CASE WHEN jsonb_typeof(value) <> 'object' THEN 'a JSON object' END
    END
$CODE$
LANGUAGE sql IMMUTABLE;

COMMENT ON FUNCTION timetable.parameter_shape_error(timetable.command_kind, TEXT, JSONB) IS
    'Returns the expected shape if value is invalid for the task kind and command, NULL otherwise';

CREATE OR REPLACE FUNCTION timetable.check_parameter() RETURNS trigger AS
$CODE$
DECLARE
    v_kind      timetable.command_kind;
    v_command   TEXT;
    v_value     JSONB;
    v_expected  TEXT;
BEGIN
    IF TG_TABLE_NAME = 'parameter' THEN
        SELECT t.kind, t.command INTO v_kind, v_command FROM timetable.task t WHERE t.task_id = NEW.task_id;
        v_value := NEW.value;
        v_expected := timetable.parameter_shape_error(v_kind, v_command, v_value);
    ELSE
        v_kind := NEW.kind;
        v_command := NEW.command;
        SELECT p.value, timetable.parameter_shape_error(v_kind, v_command, p.value) INTO v_value, v_expected
        FROM timetable.parameter p
        WHERE p.task_id = NEW.task_id AND timetable.parameter_shape_error(v_kind, v_command, p.value) IS NOT NULL
        LIMIT 1;
    END IF;
    IF v_expected IS NOT NULL THEN
        RAISE EXCEPTION 'parameter for % task % must be %',
            CASE WHEN v_kind = 'BUILTIN' THEN v_command ELSE v_kind::text END, NEW.task_id, v_expected
            USING ERRCODE = 'check_violation', DETAIL = format('value: %s', v_value);
    END IF;
    RETURN NEW;
END;
$CODE$
LANGUAGE plpgsql;

COMMENT ON FUNCTION timetable.check_parameter() IS
    'Rejects parameter values whose shape does not match the task kind or BUILTIN command';

CREATE TRIGGER check_parameter
    BEFORE INSERT OR UPDATE ON timetable.parameter
    FOR EACH ROW EXECUTE PROCEDURE timetable.check_parameter();

CREATE TRIGGER check_parameter
    BEFORE UPDATE OF kind, command ON timetable.task
    FOR EACH ROW EXECUTE PROCEDURE timetable.check_parameter();
