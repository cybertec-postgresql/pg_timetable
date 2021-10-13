CREATE OR REPLACE FUNCTION timetable.move_task_up(IN task_id BIGINT) RETURNS boolean AS $$
    WITH 
    current_task AS (
        SELECT * FROM timetable.task WHERE task_id = $1), 
    parrent_task AS (
        SELECT t.* FROM timetable.task t, current_task WHERE t.task_id = current_task.parent_id),
    upd_parent AS (
        UPDATE timetable.task t SET 
            (task_name, kind, command, run_as, database_connection, ignore_error, autonomous, timeout) = 
            (ct.task_name, ct.kind, ct.command, ct.run_as, ct.database_connection, ct.ignore_error, ct.autonomous, ct.timeout) 
        FROM current_task ct WHERE t.task_id = ct.parent_id
    ),
    upd_current AS (
        UPDATE timetable.task t SET 
            (task_name, kind, command, run_as, database_connection, ignore_error, autonomous, timeout) = 
            (pt.task_name, pt.kind, pt.command, pt.run_as, pt.database_connection, pt.ignore_error, pt.autonomous, pt.timeout) 
        FROM parrent_task pt WHERE t.task_id = $1
        RETURNING true
    )
    SELECT count(*) > 0 FROM upd_current
$$ LANGUAGE SQL;

CREATE OR REPLACE FUNCTION timetable.move_task_down(IN task_id BIGINT) RETURNS boolean AS $$
    WITH 
    current_task AS (
        SELECT * FROM timetable.task WHERE task_id = $1), 
    child_task AS (
        SELECT * FROM timetable.task WHERE parent_id = $1),
    upd_child AS (
        UPDATE timetable.task t SET 
            (task_name, kind, command, run_as, database_connection, ignore_error, autonomous, timeout) = 
            (ct.task_name, ct.kind, ct.command, ct.run_as, ct.database_connection, ct.ignore_error, ct.autonomous, ct.timeout) 
        FROM current_task ct WHERE t.parent_id = $1
    ),
    upd_current AS (
        UPDATE timetable.task t SET 
            (task_name, kind, command, run_as, database_connection, ignore_error, autonomous, timeout) = 
            (pt.task_name, pt.kind, pt.command, pt.run_as, pt.database_connection, pt.ignore_error, pt.autonomous, pt.timeout) 
        FROM child_task pt WHERE t.task_id = $1
        RETURNING true
    )
    SELECT count(*) > 0 FROM upd_current
$$ LANGUAGE SQL;

CREATE OR REPLACE FUNCTION timetable.delete_task(IN task_id BIGINT) RETURNS boolean AS $$
    WITH 
    del_task AS (
        DELETE FROM timetable.task WHERE task_id = $1 AND parent_id IS NOT NULL RETURNING parent_id),
    upd_task AS (
        UPDATE timetable.task t SET parent_id = dt.parent_id FROM del_task dt WHERE t.parent_id = $1)
    SELECT count(*) > 0 FROM del_task
$$ LANGUAGE SQL;