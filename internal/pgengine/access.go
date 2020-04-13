package pgengine

import (
	"database/sql"
	"fmt"
	"hash/adler32"
	"os"
	"os/signal"
	"syscall"
)

// InvalidOid specifies value for non-existent objects
const InvalidOid = 0

// AppID used as a key for obtaining locks on the server, it's Adler32 hash of 'pg_timetable' string
const AppID = 0x204F04EE

/*FixSchedulerCrash make sure that task chains which are not complete due to a scheduler crash are "fixed"
and marked as stopped at a certain point */
func FixSchedulerCrash() {
	_, err := ConfigDb.Exec(`
		INSERT INTO timetable.run_status (execution_status, started, last_status_update, start_status, chain_execution_config, client_name)
		  SELECT 'DEAD', now(), now(), start_status, 0, $1 FROM (
		   SELECT   start_status
		     FROM   timetable.run_status
		     WHERE   execution_status IN ('STARTED', 'CHAIN_FAILED', 'CHAIN_DONE', 'DEAD') AND client_name = $1
		     GROUP BY 1
		     HAVING count(*) < 2 ) AS abc`, ClientName)
	if err != nil {
		LogToDB("ERROR", "Error occurred during reverting from the scheduler crash: ", err)
	}
}

// CanProceedChainExecution checks if particular chain can be exeuted in parallel
func CanProceedChainExecution(chainConfigID int, maxInstances int) bool {
	const sqlProcCount = "SELECT count(*) FROM timetable.get_running_jobs($1) AS (id BIGINT, status BIGINT) GROUP BY id"
	var procCount int
	LogToDB("DEBUG", fmt.Sprintf("Checking if can proceed with chaing config ID: %d", chainConfigID))
	err := ConfigDb.Get(&procCount, sqlProcCount, chainConfigID)
	switch {
	case err == sql.ErrNoRows:
		return true
	case err == nil:
		return procCount < maxInstances
	default:
		LogToDB("ERROR", "Cannot read information about concurrent running jobs: ", err)
		return false
	}
}

// DeleteChainConfig delete chaing configuration for self destructive chains
func DeleteChainConfig(chainConfigID int) bool {
	LogToDB("LOG", "Deleting self destructive chain configuration ID: ", chainConfigID)
	res, err := ConfigDb.Exec("DELETE FROM timetable.chain_execution_config WHERE chain_execution_config = $1 ", chainConfigID)
	if err != nil {
		LogToDB("ERROR", "Error occurred during deleting self destructive chains: ", err)
	}
	rowsDeleted, err := res.RowsAffected()
	return err == nil && rowsDeleted == 1
}

// TryLockClientName obtains lock on the server to prevent another client with the same name
func TryLockClientName() (res bool) {
	adler32Int := adler32.Checksum([]byte(ClientName))
	LogToDB("DEBUG", fmt.Sprintf("Trying to get advisory lock for '%s' with hash 0x%x", ClientName, adler32Int))
	err := ConfigDb.Get(&res, "select pg_try_advisory_lock($1, $2)", AppID, adler32Int)
	if err != nil {
		LogToDB("ERROR", "Error occurred during client name locking: ", err)
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
		FinalizeConfigDBConnection()
		os.Exit(0)
	}()
}

// InsertChainRunStatus inits the execution run log, which will be use to effectively control scheduler concurrency
func InsertChainRunStatus(chainConfigID int, chainID int) int {
	const sqlInsertRunStatus = `
INSERT INTO timetable.run_status 
(chain_id, execution_status, started, chain_execution_config, client_name) 
VALUES 
($1, 'STARTED', now(), $2, $3) 
RETURNING run_status`
	var id int
	err := ConfigDb.Get(&id, sqlInsertRunStatus, chainID, chainConfigID, ClientName)
	if err != nil {
		LogToDB("ERROR", "Cannot save information about the chain run status: ", err)
	}
	return id
}

// UpdateChainRunStatus inserts status information about running chain elements
func UpdateChainRunStatus(chainElemExec *ChainElementExecution, runStatusID int, status string) {

	const sqlInsertFinishStatus = `
INSERT INTO timetable.run_status 
(chain_id, execution_status, current_execution_element, started, last_status_update, start_status, chain_execution_config, client_name)
VALUES 
($1, $2, $3, clock_timestamp(), now(), $4, $5, $6)`
	var err error

	_, err = ConfigDb.Exec(sqlInsertFinishStatus, chainElemExec.ChainID, status, chainElemExec.TaskID,
		runStatusID, chainElemExec.ChainConfig, ClientName)
	if err != nil {
		LogToDB("ERROR", "Update Chain Status failed: ", err)
	}
}
