package pgengine

import (
	"database/sql"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jmoiron/sqlx"
)

// VerboseLogLevel specifies if log messages with level LOG should be logged
var VerboseLogLevel = true

// InvalidOid specifies value for non-existent objects
const InvalidOid = 0

//GetLogPrefix perform formatted logging
func GetLogPrefix(level string) string {
	return fmt.Sprintf("[%v | %s | %-6s]:\t %%s", time.Now().Format("2006-01-01 15:04:05.000"), ClientName, level)
}

// LogToDB performs logging to configuration database ConfigDB initiated during bootstrap
func LogToDB(level string, msg ...interface{}) {
	const logTemplate = `INSERT INTO timetable.log(pid, client_name, log_level, message) VALUES ($1, $2, $3, $4)`
	if !VerboseLogLevel {
		switch level {
		case
			"DEBUG", "NOTICE":
			return
		}
	}
	s := fmt.Sprintf(GetLogPrefix(level), fmt.Sprint(msg...))
	fmt.Println(s)
	if ConfigDb != nil {
		_, err := ConfigDb.Exec(logTemplate, os.Getpid(), ClientName, level, fmt.Sprint(msg...))
		for err != nil && ConfigDb.Ping() != nil {
			// If there is DB outage, reconnect and write missing log
			ReconnectDbAndFixLeftovers()
			_, err = ConfigDb.Exec(logTemplate, os.Getpid(), ClientName, level, fmt.Sprint(msg...))
			level = "ERROR" //we don't want panic in case of disconnect
		}
	}
	if level == "PANIC" {
		panic(s)
	}
}

/*FixSchedulerCrash make sure that task chains which are not complete due to a scheduler crash are "fixed"
and marked as stopped at a certain point */
func FixSchedulerCrash() {
	_, err := ConfigDb.Exec(`
		INSERT INTO timetable.run_status (execution_status, started, last_status_update, start_status)
		  SELECT 'DEAD', now(), now(), start_status FROM (
		   SELECT   start_status
		     FROM   timetable.run_status
		     WHERE   execution_status IN ('STARTED', 'CHAIN_FAILED', 'CHAIN_DONE', 'DEAD')
		     GROUP BY 1
		     HAVING count(*) < 2 ) AS abc`)
	if err != nil {
		LogToDB("ERROR", "Error occured during reverting from the scheduler crash: ", err)
	}
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
		LogToDB("ERROR", "application cannot read information about concurrent running jobs: ", err)
		return false
	}
}

// DeleteChainConfig delete chaing configuration for self destructive chains
func DeleteChainConfig(tx *sqlx.Tx, chainConfigID int) bool {
	res, err := tx.Exec("DELETE FROM timetable.chain_execution_config WHERE chain_execution_config = $1 ", chainConfigID)
	if err != nil {
		LogToDB("ERROR", "Error occured during deleting self destructive chains: ", err)
	}
	rowsDeleted, err := res.RowsAffected()
	return err == nil && rowsDeleted == 1
}

// LogChainElementExecution will log current chain element execution status including retcode
func LogChainElementExecution(chainElemExec *ChainElementExecution, retCode int) {
	_, err := ConfigDb.Exec("INSERT INTO timetable.execution_log (chain_execution_config, chain_id, task_id, name, script, "+
		"kind, last_run, finished, returncode, pid) "+
		"VALUES ($1, $2, $3, $4, $5, $6, now(), clock_timestamp(), $7, txid_current())",
		chainElemExec.ChainConfig, chainElemExec.ChainID, chainElemExec.TaskID, chainElemExec.TaskName,
		chainElemExec.Script, chainElemExec.Kind, retCode)
	if err != nil {
		LogToDB("ERROR", "Error occured during logging current chain element execution status including retcode: ", err)
	}
}

//AddWorkerDetail Add worker to worker_status table when a worker starts
func AddWorkerDetail() {
	_, err := ConfigDb.Exec("Insert INTO timetable.worker_status (worker_name, client_name, start_time, pid) "+
		" VALUES($1, $2, now(), $3)",
		Host+"_"+Port, ClientName, os.Getpid())
	if err != nil {
		LogToDB("ERROR", "Error occured during adding worker detail: ", err)
	}
}

//RemoveWorkerDetail when a worker stopped or ended
func RemoveWorkerDetail() {
	_, err := ConfigDb.Exec("DELETE FROM timetable.worker_status WHERE worker_name = $1  AND client_name = $2 AND pid = $3",
		Host+"_"+Port, ClientName, os.Getpid())
	if err != nil {
		LogToDB("ERROR", "Error occured during removing worker: ", err)
	}
}

//IsWorkerRunning return true if already a worker is running
func IsWorkerRunning() bool {
	var exists bool
	err := ConfigDb.Get(&exists, "SELECT EXISTS(SELECT 1 FROM timetable.worker_status WHERE worker_name = $1  AND client_name = $2)",
		Host+"_"+Port, ClientName)
	if err != nil || !exists {
		return false
	}
	return true
}

// SetupCloseHandler creates a 'listener' on a new goroutine which will notify the
// program if it receives an interrupt from the OS. We then handle this by calling
// our clean up procedure and exiting the program.
func SetupCloseHandler() {
	c := make(chan os.Signal, 2)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		LogToDB("LOG", "Ctrl+C pressed at terminal")
		FinalizeConfigDBConnection()
		os.Exit(0)
	}()
}
