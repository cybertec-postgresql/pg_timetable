package pgengine

import (
	"database/sql"
	"fmt"
	"hash/adler32"
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

// AppID used as a key for obtaining locks on the server, it's Adler32 hash of 'pg_timetable' string
const AppID = 0x204F04EE

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

// TryLockClientName obtains lock on the server to prevent another client with the same name
func TryLockClientName() (res bool) {
	adler32Int := adler32.Checksum([]byte(ClientName))
	LogToDB("DEBUG", fmt.Sprintf("Trying to get advisory lock for '%s' with hash 0x%x", ClientName, adler32Int))
	err := ConfigDb.Get(&res, "select pg_try_advisory_lock($1, $2)", AppID, adler32Int)
	if err != nil {
		LogToDB("ERROR", "Error occured during client name locking: ", err)
	}
	return
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
