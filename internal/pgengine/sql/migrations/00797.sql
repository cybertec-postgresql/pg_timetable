-- B-tree index for looking up the latest executions of a specific chain
CREATE INDEX IF NOT EXISTS execution_log_chain_id_finished_idx
    ON timetable.execution_log (chain_id, finished);

-- BRIN index for time-window queries and retention cleanup
CREATE INDEX IF NOT EXISTS execution_log_finished_brin_idx
    ON timetable.execution_log USING brin (finished);
