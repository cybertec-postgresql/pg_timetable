-- pause_job() will pause the chain (set live = false)
CREATE OR REPLACE FUNCTION timetable.pause_job(IN job_name TEXT) RETURNS boolean AS $$
    WITH upd_chain AS (UPDATE timetable.chain SET live = false WHERE chain.chain_name = $1 RETURNING chain_id)
    SELECT EXISTS(SELECT 1 FROM upd_chain)
$$ LANGUAGE SQL;

COMMENT ON FUNCTION timetable.pause_job IS 'Pause the chain (set live = false)';

-- resume_job() will resume the chain (set live = true)
CREATE OR REPLACE FUNCTION timetable.resume_job(IN job_name TEXT) RETURNS boolean AS $$
    WITH upd_chain AS (UPDATE timetable.chain SET live = true WHERE chain.chain_name = $1 RETURNING chain_id)
    SELECT EXISTS(SELECT 1 FROM upd_chain)
$$ LANGUAGE SQL;

COMMENT ON FUNCTION timetable.resume_job IS 'Resume the chain (set live = true)';