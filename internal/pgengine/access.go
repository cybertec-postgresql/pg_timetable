package pgengine

import (
	"database/sql"
	"fmt"
	"os"
	"time"

	"github.com/jmoiron/sqlx"
)

// VerboseLogLevel specifies if log messages with level LOG should be logged
var VerboseLogLevel = true

// InvalidOid specifies value for non-existent objects
const InvalidOid = 0

// LogToDB performs logging to configuration database ConfigDB initiated during bootstrap
func LogToDB(level string, msg ...interface{}) {
	if !VerboseLogLevel {
		switch level {
		case
			"DEBUG", "NOTICE", "LOG":
			return
		}
	}
	if ConfigDb != nil {
		ConfigDb.MustExec(`INSERT INTO timetable.log(pid, client_name, log_level, message) VALUES ($1, $2, $3, $4)`,
			os.Getpid(), ClientName, level, fmt.Sprint(msg...))
	}
	s := fmt.Sprintf("[%v | %s | %-6s]:\t %s", time.Now().Format("2006-01-01 15:04:05.000"), ClientName, level, fmt.Sprint(msg...))
	fmt.Println(s)
	if level == "PANIC" {
		panic(s)
	}
}

/*FixSchedulerCrash make sure that task chains which are not complete due to a scheduler crash are "fixed"
and marked as stopped at a certain point */
func FixSchedulerCrash() {
	ConfigDb.MustExec(`
INSERT INTO timetable.run_status (execution_status, started, last_status_update, start_status)
  SELECT 'DEAD', now(), now(), start_status FROM (
   SELECT   start_status
     FROM   timetable.run_status
     WHERE   execution_status IN ('STARTED', 'CHAIN_FAILED', 'CHAIN_DONE', 'DEAD')
     GROUP BY 1
     HAVING count(*) < 2 ) AS abc`)
}

// CanProceedChainExecution checks if particular chain can be exeuted in parallel
func CanProceedChainExecution(chainConfigID int, maxInstances int) bool {
	const sqlProcCount = "SELECT count(*) FROM timetable.get_running_jobs($1) AS (id BIGINT, status BIGINT) GROUP BY id"
	var procCount int
	LogToDB("DEBUG", fmt.Sprintf("checking if can proceed with chaing config id: %d", chainConfigID))
	err := ConfigDb.Get(&procCount, sqlProcCount, chainConfigID)
	switch {
	case err == sql.ErrNoRows:
		return true
	case err == nil:
		return procCount < maxInstances
	default:
		LogToDB("PANIC", "application cannot read information about concurrent running jobs: ", err)
		return false
	}
}

// DeleteChainConfig delete chaing configuration for self destructive chains
func DeleteChainConfig(tx *sqlx.Tx, chainConfigID int) bool {
	res := tx.MustExec("DELETE FROM timetable.chain_execution_config WHERE chain_execution_config = $1 ",
		chainConfigID)
	rowsDeleted, err := res.RowsAffected()
	return err == nil && rowsDeleted == 1
}

// LogChainElementExecution will log current chain element execution status including retcode
func LogChainElementExecution(chainElemExec *ChainElementExecution, retCode int) {
	ConfigDb.MustExec("INSERT INTO timetable.execution_log (chain_execution_config, chain_id, task_id, name, script, "+
		"kind, last_run, finished, returncode, pid) "+
		"VALUES ($1, $2, $3, $4, $5, $6, now(), clock_timestamp(), $7, txid_current())",
		chainElemExec.ChainConfig, chainElemExec.ChainID, chainElemExec.TaskID, chainElemExec.TaskName,
		chainElemExec.Script, chainElemExec.Kind, retCode)
}
