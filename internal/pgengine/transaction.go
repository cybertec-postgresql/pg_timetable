package pgengine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

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
	StartedAt          time.Time
	Duration           int64 // in microseconds
}

func (chainElem ChainElementExecution) String() string {
	data, _ := json.Marshal(chainElem)
	return string(data)
}

// StartTransaction return transaction object and panic in the case of error
func StartTransaction(ctx context.Context) (*sqlx.Tx, error) {
	return ConfigDb.BeginTxx(ctx, nil)
}

// MustCommitTransaction commits transaction and log error in the case of error
func MustCommitTransaction(tx *sqlx.Tx) {
	LogToDB("DEBUG", "Commit transaction for successful chain execution")
	err := tx.Commit()
	if err != nil {
		LogToDB("ERROR", "Application cannot commit after job finished: ", err)
	}
}

// MustRollbackTransaction rollbacks transaction and log error in the case of error
func MustRollbackTransaction(tx *sqlx.Tx) {
	LogToDB("DEBUG", "Rollback transaction for failed chain execution")
	err := tx.Rollback()
	if err != nil {
		LogToDB("ERROR", "Application cannot rollback after job failed: ", err)
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
		LogToDB("ERROR", "Recursive queries to fetch chain tasks failed: ", err)
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
		LogToDB("ERROR", "cannot fetch parameters values for chain: ", err)
		return false
	}
	return true
}

// ExecuteSQLTask executes SQL task
func ExecuteSQLTask(ctx context.Context, tx *sqlx.Tx, chainElemExec *ChainElementExecution, paramValues []string) error {
	var execTx *sqlx.Tx
	var remoteDb *sqlx.DB

	execTx = tx
	//Connect to Remote DB
	if chainElemExec.DatabaseConnection.Valid {
		connectionString := GetConnectionString(chainElemExec.DatabaseConnection)
		//connection string is empty then don't proceed
		if strings.TrimSpace(connectionString) == "" {
			return errors.New("Connection string is blank")
		}
		remoteDb, execTx = GetRemoteDBTransaction(ctx, connectionString)
		//don't proceed when remote db connection not established
		if execTx == nil {
			return errors.New("Couldn't connect to remote database")
		}
		defer FinalizeRemoteDBConnection(remoteDb)
	}

	// Set Role
	if chainElemExec.RunUID.Valid {
		SetRole(execTx, chainElemExec.RunUID)
	}

	if chainElemExec.IgnoreError {
		LogToDB("DEBUG", "Define savepoint to ignore an error for the task: ", chainElemExec.TaskName)
		_, err := execTx.Exec("SAVEPOINT " + strconv.Quote(chainElemExec.TaskName))
		if err != nil {
			LogToDB("ERROR", err)
		}
	}

	err := ExecuteSQLCommand(execTx, chainElemExec.Script, paramValues)

	if err != nil && chainElemExec.IgnoreError {
		LogToDB("DEBUG", "Rollback to savepoint ignoring error for the task: ", chainElemExec.TaskName)
		_, err := execTx.Exec("ROLLBACK TO SAVEPOINT " + strconv.Quote(chainElemExec.TaskName))
		if err != nil {
			LogToDB("ERROR", err)
		}
	}

	//Reset The Role
	if chainElemExec.RunUID.Valid {
		ResetRole(execTx)
	}

	// Commit changes on remote server
	if chainElemExec.DatabaseConnection.Valid {
		MustCommitTransaction(execTx)
	}

	return err
}

// ExecuteSQLCommand executes chain script with parameters inside transaction
func ExecuteSQLCommand(tx *sqlx.Tx, script string, paramValues []string) error {
	var err error
	var params []interface{}

	if strings.TrimSpace(script) == "" {
		return errors.New("SQL script cannot be empty")
	}
	if len(paramValues) == 0 { //mimic empty param
		_, err = tx.Exec(script)
	} else {
		for _, val := range paramValues {
			if val > "" {
				if err := json.Unmarshal([]byte(val), &params); err != nil {
					return err
				}
				LogToDB("DEBUG", "Executing the command: ", script, fmt.Sprintf("; With parameters: %+v", params))
				_, err = tx.Exec(script, params...)
			}
		}
	}
	return err
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
func GetRemoteDBTransaction(ctx context.Context, connectionString string) (*sqlx.DB, *sqlx.Tx) {
	remoteDb, err := sqlx.ConnectContext(ctx, "postgres", connectionString)
	if err != nil {
		LogToDB("ERROR",
			fmt.Sprintf("Error in remote connection (%s): %v", connectionString, err))
		return nil, nil
	}
	LogToDB("LOG", "Remote Connection established...")
	remoteTx, err := remoteDb.BeginTxx(ctx, nil)
	if err != nil {
		LogToDB("ERROR",
			fmt.Sprintf("Error during start of remote transaction (%s): %v", connectionString, err))
		return nil, nil
	}
	return remoteDb, remoteTx
}

// FinalizeRemoteDBConnection closes session
func FinalizeRemoteDBConnection(remoteDb *sqlx.DB) {
	LogToDB("LOG", "Closing remote session")
	if err := remoteDb.Close(); err != nil {
		LogToDB("ERROR", "Cannot close database connection:", err)
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
