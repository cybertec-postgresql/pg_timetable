package pgengine

import (
	"context"
	"fmt"
	"os"

	"github.com/georgysavva/scany/pgxscan"
)

// InvalidOid specifies value for non-existent objects
const InvalidOid = 0

// DeleteChainConfig delete chain configuration for self destructive chains
func (pge *PgEngine) DeleteChainConfig(ctx context.Context, chainID int) bool {
	pge.l.WithField("chain", chainID).Info("Deleting self destructive chain configuration")
	res, err := pge.ConfigDb.Exec(ctx, "DELETE FROM timetable.chain WHERE chain_id = $1", chainID)
	if err != nil {
		pge.l.WithError(err).Error("Failed to delete self destructive chain")
		return false
	}
	return err == nil && res.RowsAffected() == 1
}

// IsAlive returns true if the connection to the database is alive
func (pge *PgEngine) IsAlive() bool {
	return pge.ConfigDb != nil && pge.ConfigDb.Ping(context.Background()) == nil
}

// LogChainElementExecution will log current chain element execution status including retcode
func (pge *PgEngine) LogChainElementExecution(ctx context.Context, task *ChainTask, retCode int, output string) {
	_, err := pge.ConfigDb.Exec(ctx, `INSERT INTO timetable.execution_log (
chain_id, task_id, command, kind, last_run, finished, returncode, pid, output, client_name) 
VALUES ($1, $2, $3, $4, clock_timestamp() - $5 :: interval, clock_timestamp(), $6, $7, NULLIF($8, ''), $9)`,
		task.ChainID, task.TaskID, task.Script, task.Kind,
		fmt.Sprintf("%f seconds", float64(task.Duration)/1000000),
		retCode, os.Getpid(), output, pge.ClientName)
	if err != nil {
		pge.l.WithError(err).Error("Failed to log chain element execution status")
	}
}

// InsertChainRunStatus inits the execution run log, which will be use to effectively control scheduler concurrency
func (pge *PgEngine) InsertChainRunStatus(ctx context.Context, chainID int, maxInstances int) bool {
	const sqlInsertRunStatus = `INSERT INTO timetable.active_chain (chain_id, client_name) 
SELECT $1, $2 WHERE
	(
		SELECT COALESCE(count(*) < $3, TRUE) 
		FROM timetable.active_chain ac WHERE ac.chain_id = $1
	)`
	res, err := pge.ConfigDb.Exec(ctx, sqlInsertRunStatus, chainID, pge.ClientName, maxInstances)
	if err != nil {
		pge.l.WithError(err).Error("Cannot save information about the chain run status")
		return false
	}
	return res.RowsAffected() == 1
}

func (pge *PgEngine) RemoveChainRunStatus(ctx context.Context, chainID int) {
	const sqlRemoveRunStatus = `DELETE FROM timetable.active_chain WHERE chain_id = $1 and client_name = $2`
	_, err := pge.ConfigDb.Exec(ctx, sqlRemoveRunStatus, chainID, pge.ClientName)
	if err != nil {
		pge.l.WithError(err).Error("Cannot save information about the chain run status")
	}
	return
}

//Select live chains with proper client_name value
const sqlSelectLiveChains = `SELECT chain_id, chain_name, self_destruct, exclusive_execution, timeout, COALESCE(max_instances, 16) as max_instances
FROM timetable.chain WHERE live AND (client_name = $1 or client_name IS NULL)`

// SelectRebootChains returns a list of chains should be executed after reboot
func (pge *PgEngine) SelectRebootChains(ctx context.Context, dest interface{}) error {
	const sqlSelectRebootChains = sqlSelectLiveChains + ` AND run_at = '@reboot'`
	return pgxscan.Select(ctx, pge.ConfigDb, dest, sqlSelectRebootChains, pge.ClientName)
}

// SelectChains returns a list of chains should be executed at the current moment
func (pge *PgEngine) SelectChains(ctx context.Context, dest interface{}) error {
	const sqlSelectChains = sqlSelectLiveChains + ` AND NOT COALESCE(starts_with(run_at, '@'), FALSE) AND timetable.is_cron_in_time(run_at, now())`
	return pgxscan.Select(ctx, pge.ConfigDb, dest, sqlSelectChains, pge.ClientName)
}

// SelectIntervalChains returns list of interval chains to be executed
func (pge *PgEngine) SelectIntervalChains(ctx context.Context, dest interface{}) error {
	const sqlSelectIntervalChains = `SELECT
    chain_id, chain_name, self_destruct, exclusive_execution, 
    timeout, COALESCE(max_instances, 16) as max_instances,
    EXTRACT(EPOCH FROM (substr(run_at, 7) :: interval)) :: int4 as interval_seconds,
    starts_with(run_at, '@after') as repeat_after
FROM 
    timetable.chain 
WHERE 
    live AND (client_name = $1 or client_name IS NULL) AND substr(run_at, 1, 6) IN ('@every', '@after')`
	return pgxscan.Select(ctx, pge.ConfigDb, dest, sqlSelectIntervalChains, pge.ClientName)
}

// SelectChain returns the chain with the specified ID
func (pge *PgEngine) SelectChain(ctx context.Context, dest interface{}, chainID int) error {
	// we accept not only live chains here because we want to run them in debug mode
	const sqlSelectSingleChain = `SELECT chain_id, chain_name, self_destruct, exclusive_execution, timeout, COALESCE(max_instances, 16) as max_instances
FROM
    timetable.chain 
WHERE 
    (client_name = $1 OR client_name IS NULL) AND chain_id = $2`
	return pgxscan.Get(ctx, pge.ConfigDb, dest, sqlSelectSingleChain, pge.ClientName, chainID)
}
