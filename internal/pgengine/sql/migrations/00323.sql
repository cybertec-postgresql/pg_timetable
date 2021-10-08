CREATE OR REPLACE FUNCTION timetable.delete_job(IN job_name TEXT) RETURNS boolean AS $$
    WITH
    del_chain AS (
        DELETE FROM timetable.chain WHERE chain.chain_name = $1 RETURNING task_id),
    del_tasks AS (
		DELETE FROM timetable.task WHERE task.task_id IN (SELECT task_id FROM del_chain)
	)
    SELECT count(*) > 0 FROM del_chain
$$ LANGUAGE SQL;
