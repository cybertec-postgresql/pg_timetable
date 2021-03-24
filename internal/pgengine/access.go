package pgengine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/georgysavva/scany/pgxscan"
	pgx "github.com/jackc/pgx/v4"
)

// InvalidOid specifies value for non-existent objects
const InvalidOid = 0

// AppID used as a key for obtaining locks on the server, it's Adler32 hash of 'pg_timetable' string
const AppID = 0x204F04EE

/*FixSchedulerCrash make sure that task chains which are not complete due to a scheduler crash are "fixed"
and marked as stopped at a certain point */
func (pge *PgEngine) FixSchedulerCrash(ctx context.Context) {
	_, err := pge.ConfigDb.Exec(ctx, `SELECT timetable.health_check($1)`, pge.ClientName)
	if err != nil {
		pge.l.WithError(err).Error("Failed to perform health check")
	}
}

// CanProceedChainExecution checks if particular chain can be exeuted in parallel
func (pge *PgEngine) CanProceedChainExecution(ctx context.Context, chainConfigID int, maxInstances int) bool {
	if ctx.Err() != nil {
		return false
	}
	const sqlProcCount = "SELECT count(*) FROM timetable.get_running_jobs($1) AS (id BIGINT, status BIGINT) GROUP BY id"
	var procCount int
	err := pge.ConfigDb.QueryRow(ctx, sqlProcCount, chainConfigID).Scan(&procCount)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return true
	case err == nil:
		return procCount < maxInstances
	default:
		pge.l.WithError(err).Error("Cannot read information about concurrent running jobs")
		return false
	}
}

// DeleteChainConfig delete chaing configuration for self destructive chains
func (pge *PgEngine) DeleteChainConfig(ctx context.Context, chainConfigID int) bool {
	pge.l.WithField("chain", chainConfigID).Info("Deleting self destructive chain configuration")
	res, err := pge.ConfigDb.Exec(ctx, "DELETE FROM timetable.chain_execution_config WHERE chain_execution_config = $1", chainConfigID)
	if err != nil {
		pge.l.WithError(err).Error("Failed to delete self destructive chain")
		return false
	}
	return err == nil && res.RowsAffected() == 1
}

// SetupCloseHandler creates a 'listener' on a new goroutine which will notify the
// program if it receives an interrupt from the OS. We then handle this by calling
// our clean up procedure and exiting the program.
func (pge *PgEngine) SetupCloseHandler() {
	c := make(chan os.Signal, 2)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		pge.Finalize()
		os.Exit(0)
	}()
}

// IsAlive returns true if the connection to the database is alive
func (pge *PgEngine) IsAlive() bool {
	return pge.ConfigDb != nil && pge.ConfigDb.Ping(context.Background()) == nil
}

// LogChainElementExecution will log current chain element execution status including retcode
func (pge *PgEngine) LogChainElementExecution(ctx context.Context, chainElemExec *ChainElementExecution, retCode int, output string) {
	_, err := pge.ConfigDb.Exec(ctx, "INSERT INTO timetable.execution_log (chain_execution_config, chain_id, task_id, name, script, "+
		"kind, last_run, finished, returncode, pid, output, client_name) "+
		"VALUES ($1, $2, $3, $4, $5, $6, clock_timestamp() - $7 :: interval, clock_timestamp(), $8, $9, "+
		"NULLIF($10, ''), $11)",
		chainElemExec.ChainConfig, chainElemExec.ChainID, chainElemExec.TaskID, chainElemExec.TaskName,
		chainElemExec.Script, chainElemExec.Kind,
		fmt.Sprintf("%f seconds", float64(chainElemExec.Duration)/1000000),
		retCode, os.Getpid(), output, pge.ClientName)
	if err != nil {
		pge.l.WithError(err).Error("Failed to log chain element execution status")
	}
}

// InsertChainRunStatus inits the execution run log, which will be use to effectively control scheduler concurrency
func (pge *PgEngine) InsertChainRunStatus(ctx context.Context, chainConfigID int, chainID int) int {
	const sqlInsertRunStatus = `
INSERT INTO timetable.run_status 
(chain_id, execution_status, started, chain_execution_config, client_name) 
VALUES 
($1, 'STARTED', now(), $2, $3) 
RETURNING run_status`
	var id int
	err := pge.ConfigDb.QueryRow(ctx, sqlInsertRunStatus, chainID, chainConfigID, pge.ClientName).Scan(&id)
	if err != nil {
		pge.l.WithError(err).Error("Cannot save information about the chain run status")
	}
	return id
}

// UpdateChainRunStatus inserts status information about running chain elements
func (pge *PgEngine) UpdateChainRunStatus(ctx context.Context, chainElemExec *ChainElementExecution, runStatusID int, status string) {
	const sqlInsertFinishStatus = `INSERT INTO timetable.run_status 
(chain_id, execution_status, current_execution_element, started, last_status_update, start_status, chain_execution_config, client_name)
VALUES 
($1, $2, $3, clock_timestamp(), now(), $4, $5, $6)`
	var err error
	_, err = pge.ConfigDb.Exec(ctx, sqlInsertFinishStatus, chainElemExec.ChainID, status, chainElemExec.TaskID,
		runStatusID, chainElemExec.ChainConfig, pge.ClientName)
	if err != nil {
		pge.l.WithError(err).Error("Update chain status failed")
	}
}

//Select live chains with proper client_name value
const sqlSelectLiveChains = `SELECT
	chain_execution_config, chain_id, chain_name, self_destruct, exclusive_execution, COALESCE(max_instances, 16) as max_instances
FROM 
	timetable.chain_execution_config 
WHERE 
	live 
	AND chain_id IS NOT NULL 
	AND (client_name = $1 or client_name IS NULL)`

func qualifySQL(sql string) string {
	// for future use
	return sql
}

// SelectRebootChains returns a list of chains should be executed after reboot
func (pge *PgEngine) SelectRebootChains(ctx context.Context, dest interface{}) error {
	const sqlSelectRebootChains = sqlSelectLiveChains + ` AND run_at = '@reboot'`
	return pgxscan.Select(ctx, pge.ConfigDb, dest, qualifySQL(sqlSelectRebootChains), pge.ClientName)
}

// SelectRebootChains returns a list of chains should be executed after reboot
func (pge *PgEngine) SelectChains(ctx context.Context, dest interface{}) error {
	const sqlSelectChains = sqlSelectLiveChains + ` AND NOT COALESCE(starts_with(run_at, '@'), FALSE) AND timetable.is_cron_in_time(run_at, now())`
	return pgxscan.Select(ctx, pge.ConfigDb, dest, qualifySQL(sqlSelectChains), pge.ClientName)
}

// SelectIntervalChains returns list of interval chains to be executed
func (pge *PgEngine) SelectIntervalChains(ctx context.Context, dest interface{}) error {
	const sqlSelectIntervalChains = `SELECT
	chain_execution_config, chain_id, chain_name, self_destruct, exclusive_execution, COALESCE(max_instances, 16) as max_instances,
	EXTRACT(EPOCH FROM (substr(run_at, 7) :: interval)) :: int4 as interval_seconds,
	starts_with(run_at, '@after') as repeat_after
FROM 
	timetable.chain_execution_config 
WHERE 
	live AND (client_name = $1 or client_name IS NULL) AND substr(run_at, 1, 6) IN ('@every', '@after')`
	return pgxscan.Select(ctx, pge.ConfigDb, dest, qualifySQL(sqlSelectIntervalChains), pge.ClientName)
}

// SelectChain returns the chain with the specified ID
func (pge *PgEngine) SelectChain(ctx context.Context, dest interface{}, chainID int) error {
	// we accept not only live chains here because we want to run them in debug mode
	const sqlSelectSingleChain = `SELECT
	chain_execution_config, chain_id, chain_name, self_destruct, exclusive_execution, COALESCE(max_instances, 16) as max_instances
FROM 
	timetable.chain_execution_config 
WHERE 
	chain_id IS NOT NULL
	AND (client_name = $1 or client_name IS NULL) 
	AND chain_execution_config = $2`
	return pgxscan.Get(ctx, pge.ConfigDb, dest, qualifySQL(sqlSelectSingleChain), pge.ClientName, chainID)
}
