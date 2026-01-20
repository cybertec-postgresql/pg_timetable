package pgengine

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// InvalidOid specifies value for non-existent objects
const InvalidOid = 0

// DeleteChain delete chain configuration for self destructive chains
func (pge *PgEngine) DeleteChain(ctx context.Context, chainID int) bool {
	pge.l.WithField("chain", chainID).Info("Deleting self destructive chain configuration")
	res, err := pge.ConfigDb.Exec(ctx, "DELETE FROM timetable.chain WHERE chain_id = $1", chainID)
	if err != nil {
		pge.l.WithError(err).Error("Failed to delete self destructive chain")
		return false
	}
	return res.RowsAffected() == 1
}

// IsAlive returns true if the connection to the database is alive
func (pge *PgEngine) IsAlive() bool {
	return pge.ConfigDb != nil && pge.ConfigDb.Ping(context.Background()) == nil
}

// LogTaskExecution will log current chain element execution status including retcode
func (pge *PgEngine) LogTaskExecution(ctx context.Context, task *ChainTask, retCode int, output string, params string) {
	switch pge.Logging.LogDBLevel {
	case "none":
		return
	case "error":
		if task.IgnoreError || retCode == 0 {
			return
		}
	}
	_, err := pge.ConfigDb.Exec(ctx, `INSERT INTO timetable.execution_log (
chain_id, task_id, command, kind, last_run, finished, returncode, pid, output, client_name, txid, ignore_error, params) 
VALUES ($1, $2, $3, $4, clock_timestamp() - $5 :: interval, clock_timestamp(), $6, $7, NULLIF($8, ''), $9, $10, $11, $12)`,
		task.ChainID, task.TaskID, task.Command, task.Kind,
		fmt.Sprintf("%f seconds", float64(task.Duration)/1000000),
		retCode, pge.Getsid(), strings.TrimSpace(output), pge.ClientName, task.Vxid,
		task.IgnoreError, params)
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
}

// Select live chains with proper client_name value
const sqlSelectLiveChains = `SELECT chain_id, chain_name, self_destruct, exclusive_execution, 
COALESCE(max_instances, 16) as max_instances, COALESCE(timeout, 0) as timeout, COALESCE(on_error, '') as on_error
FROM timetable.chain WHERE live AND (client_name = $1 or client_name IS NULL)`

// SelectRebootChains returns a list of chains should be executed after reboot
func (pge *PgEngine) SelectRebootChains(ctx context.Context, dest *[]Chain) error {
	const sqlSelectRebootChains = sqlSelectLiveChains + ` AND run_at = '@reboot'`
	rows, err := pge.ConfigDb.Query(ctx, sqlSelectRebootChains, pge.ClientName)
	if err != nil {
		return err
	}
	*dest, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Chain])
	return err
}

// SelectChains returns a list of chains should be executed at the current moment
func (pge *PgEngine) SelectChains(ctx context.Context, dest *[]Chain) error {
	const sqlSelectChains = sqlSelectLiveChains + ` AND NOT COALESCE(starts_with(run_at, '@'), FALSE) AND timetable.is_cron_in_time(run_at, now())`
	rows, err := pge.ConfigDb.Query(ctx, sqlSelectChains, pge.ClientName)
	if err != nil {
		return err
	}
	*dest, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Chain])
	return err
}

// SelectIntervalChains returns list of interval chains to be executed
func (pge *PgEngine) SelectIntervalChains(ctx context.Context, dest *[]IntervalChain) error {
	const sqlSelectIntervalChains = `SELECT chain_id, chain_name, self_destruct, exclusive_execution, 
COALESCE(max_instances, 16), COALESCE(timeout, 0), COALESCE(on_error, '') as on_error,
EXTRACT(EPOCH FROM (substr(run_at, 7) :: interval)) :: int4 as interval_seconds,
starts_with(run_at, '@after') as repeat_after
FROM timetable.chain WHERE live AND (client_name = $1 or client_name IS NULL) AND substr(run_at, 1, 6) IN ('@every', '@after')`
	rows, err := pge.ConfigDb.Query(ctx, sqlSelectIntervalChains, pge.ClientName)
	if err != nil {
		return err
	}
	*dest, err = pgx.CollectRows(rows, pgx.RowToStructByPos[IntervalChain])
	return err
}

// SelectChain returns the chain with the specified ID
func (pge *PgEngine) SelectChain(ctx context.Context, dest *Chain, chainID int) error {
	// we accept not only live chains here because we want to run them in debug mode
	const sqlSelectSingleChain = `SELECT chain_id, chain_name, self_destruct, exclusive_execution, 
COALESCE(timeout, 0) as timeout, COALESCE(max_instances, 16) as max_instances, COALESCE(on_error, '') as on_error
FROM timetable.chain WHERE (client_name = $1 OR client_name IS NULL) AND chain_id = $2`
	rows, err := pge.ConfigDb.Query(ctx, sqlSelectSingleChain, pge.ClientName, chainID)
	if err != nil {
		return err
	}
	*dest, err = pgx.CollectOneRow(rows, pgx.RowToStructByName[Chain])
	return err
}

// GetChainElements returns all elements for a given chain
func (pge *PgEngine) GetChainElements(ctx context.Context, chainTasks *[]ChainTask, chainID int) error {
	const sqlSelectChainTasks = `SELECT 
	task_id,
	command,
	kind,
	COALESCE(run_as, '') as run_as,
	ignore_error,
	autonomous,
	COALESCE(database_connection, '') as database_connection,
	timeout
FROM timetable.task WHERE chain_id = $1 ORDER BY task_order ASC`
	rows, err := pge.ConfigDb.Query(ctx, sqlSelectChainTasks, chainID)
	if err != nil {
		return err
	}
	*chainTasks, err = pgx.CollectRows(rows, pgx.RowToStructByName[ChainTask])
	return err
}

// GetChainParamValues returns parameter values to pass for task being executed
func (pge *PgEngine) GetChainParamValues(ctx context.Context, paramValues *[]string, task *ChainTask) error {
	const sqlGetParamValues = `SELECT value FROM timetable.parameter WHERE task_id = $1 AND value IS NOT NULL ORDER BY order_id ASC`
	rows, err := pge.ConfigDb.Query(ctx, sqlGetParamValues, task.TaskID)
	if err != nil {
		return err
	}
	*paramValues, err = pgx.CollectRows(rows, pgx.RowTo[string])
	return err
}
