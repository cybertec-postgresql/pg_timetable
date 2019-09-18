package pgengine

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

// ChainElementExecution structure describes each chain execution process
type ChainElementExecution struct {
	ChainConfig        int            `db:"chain_config"`
	ChainID            int            `db:"chain_id"`
	TaskID             int            `db:"task_id"`
	TaskName           string         `db:"task_name"`
	Script             string         `db:"script"`
	Kind               string         `db:"kind"`
	RunUID             sql.NullString `db:"run_uid"`
	IgnoreError        bool           `db:"ignore_error"`
	DatabaseConnection sql.NullString `db:"database_connection"`
	ConnectString      sql.NullString `db:"connect_string"`
}

func (chainElem ChainElementExecution) String() string {
	data, _ := json.Marshal(chainElem)
	return string(data)
}

// StartTransaction return transaction object and panic in the case of error
func StartTransaction() *sqlx.Tx {
	return ConfigDb.MustBegin()
}

// MustCommitTransaction commits transaction and log panic in the case of error
func MustCommitTransaction(tx *sqlx.Tx) {
	defer SoftPanic("Commit Transaction failed ")
	err := tx.Commit()
	if err != nil {
		LogToDB("PANIC", "Application cannot commit after job finished: ", err)
	}
}

// GetChainElements returns all elements for a given chain
func GetChainElements(tx *sqlx.Tx, chains interface{}, chainID int) bool {
	const sqlSelectChains = `
WITH RECURSIVE x
(chain_id, task_id, task_name, script, kind, run_uid, ignore_error, database_connection) AS 
(
	SELECT tc.chain_id, tc.task_id, bt.name, 
	bt.script, bt.kind, 
	tc.run_uid, 
	tc.ignore_error, 
	tc.database_connection 
	FROM timetable.task_chain tc JOIN 
	timetable.base_task bt USING (task_id) 
	WHERE tc.parent_id IS NULL AND tc.chain_id = $1 
	UNION ALL 
	SELECT tc.chain_id, tc.task_id, bt.name, 
	bt.script, bt.kind, 
	tc.run_uid, 
	tc.ignore_error, 
	tc.database_connection 
	FROM timetable.task_chain tc JOIN 
	timetable.base_task bt USING (task_id) JOIN 
	x ON (x.chain_id = tc.parent_id) 
) 
	SELECT *, (
		SELECT connect_string 
		FROM   timetable.database_connection AS a 
		WHERE a.database_connection = x.database_connection) 
	FROM x`

	err := tx.Select(chains, sqlSelectChains, chainID)

	if err != nil {
		LogToDB("PANIC", "Recursive queries to fetch task chain failed: ", err)
		return false
	}
	return true
}

// GetChainParamValues returns parameter values to pass for task being executed
func GetChainParamValues(tx *sqlx.Tx, paramValues interface{}, chainElemExec *ChainElementExecution) bool {
	const sqlGetParamValues = `
SELECT value
FROM  timetable.chain_execution_parameters
WHERE chain_execution_config = $1
  AND chain_id = $2
ORDER BY order_id ASC`
	err := tx.Select(paramValues, sqlGetParamValues, chainElemExec.ChainConfig, chainElemExec.ChainID)
	if err != nil {
		LogToDB("PANIC", "cannot fetch parameters values for chain: ", err)
		return false
	}
	return true
}

// ExecuteSQLCommand executes chain script with parameters inside transaction
func ExecuteSQLCommand(tx *sqlx.Tx, script string, paramValues []string) error {
	var err error
	var res sql.Result
	var params []interface{}

	if script == "" {
		return errors.New("SQL script cannot be empty")
	}
	if len(paramValues) == 0 { //mimic empty param
		res, err = tx.Exec(script)
	} else {
		for _, val := range paramValues {
			if val > "" {
				if err := json.Unmarshal([]byte(val), &params); err != nil {
					return err
				}
				LogToDB("DEBUG", "Executing the command: ", script, fmt.Sprintf("\nWith parameters: %+v", params))
				res, err = tx.Exec(script, params...)
			}
		}
	}
	if err != nil {
		return err
	}
	cnt, _ := res.RowsAffected()
	LogToDB("LOG", "Successfully executed command: ", script, "\nAffected: ", cnt)
	return nil
}

// InsertChainRunStatus inits the execution run log, which will be use to effectively control scheduler concurrency
func InsertChainRunStatus(tx *sqlx.Tx, chainConfigID int, chainID int) int {
	const sqlInsertRunStatus = `
INSERT INTO timetable.run_status 
(chain_id, execution_status, started, start_status, chain_execution_config) 
VALUES 
($1, 'STARTED', now(), currval('timetable.run_status_run_status_seq'), $2) 
RETURNING run_status`
	var id int
	err := tx.Get(&id, sqlInsertRunStatus, chainID, chainConfigID)
	if err != nil {
		LogToDB("ERROR", "Cannot save information about the chain run status: ", err)
	}
	return id
}

// UpdateChainRunStatus inserts status information about running chain elements
func UpdateChainRunStatus(tx *sqlx.Tx, chainElemExec *ChainElementExecution, runStatusID int, status string) {
	const sqlInsertFinishStatus = `
INSERT INTO timetable.run_status 
(chain_id, execution_status, current_execution_element, started, last_status_update, start_status, chain_execution_config)
VALUES 
($1, $2, $3, clock_timestamp(), now(), $4, $5)`
	defer SoftPanic("Update Chain Status failed ")
	tx.MustExec(sqlInsertFinishStatus, chainElemExec.ChainID, status, chainElemExec.TaskID, runStatusID, chainElemExec.ChainConfig)
}

//GetConnectionString of database_connection
func GetConnectionString(databaseConnection sql.NullString) (connectionString string) {
	rows := ConfigDb.QueryRow("SELECT connect_string FROM  timetable.database_connection WHERE database_connection = $1", databaseConnection)
	err := rows.Scan(&connectionString)
	if err != nil {
		LogToDB("ERROR", "Issue while fetching connection string:", err)
	}
	return connectionString
}

//GetRemoteDBTransaction create a remote db connection and returns transaction object
func GetRemoteDBTransaction(connectionString string) (*sqlx.DB, *sqlx.Tx) {
	remoteDb, err := sqlx.Connect("postgres", connectionString)
	if err != nil {
		LogToDB("ERROR", fmt.Sprintf("Error in remote connection %v\n", connectionString))
		return nil, nil
	}
	LogToDB("LOG", "Remote Connection established...")
	return remoteDb, remoteDb.MustBegin()
}

// FinalizeRemoteDBConnection closes session
func FinalizeRemoteDBConnection(remoteDb *sqlx.DB) {
	LogToDB("LOG", "Closing remote session")
	if err := remoteDb.Close(); err != nil {
		LogToDB("PANIC", "Cannot close database connection:", err)
	}
	remoteDb = nil
}

// SetRole - set the current user identifier of the current session
func SetRole(tx *sqlx.Tx, runUID sql.NullString) {
	LogToDB("LOG", "Setting Role to ", runUID.String)
	_, err := tx.Exec(fmt.Sprintf("SET ROLE %v", runUID.String))
	if err != nil {
		LogToDB("ERROR", "Error in Setting role", err)
	}
}

//ResetRole - RESET forms reset the current user identifier to be the current session user identifier
func ResetRole(tx *sqlx.Tx) {
	LogToDB("LOG", "Resetting Role")
	const sqlResetRole = `RESET ROLE`
	_, err := tx.Exec(sqlResetRole)
	if err != nil {
		LogToDB("ERROR", "Error in ReSetting role", err)
	}
}
