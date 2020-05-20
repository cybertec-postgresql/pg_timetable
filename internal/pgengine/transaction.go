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
	Autonomous         bool           `db:"autonomous"`
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

func mustSavepoint(tx *sqlx.Tx, savepoint string) {
	LogToDB("DEBUG", "Define savepoint to ignore an error for the task: ", strconv.Quote(savepoint))
	_, err := tx.Exec("SAVEPOINT " + strconv.Quote(savepoint))
	if err != nil {
		LogToDB("ERROR", err)
	}
}

func mustRollbackToSavepoint(tx *sqlx.Tx, savepoint string) {
	LogToDB("DEBUG", "Rollback to savepoint ignoring error for the task: ", savepoint)
	_, err := tx.Exec("ROLLBACK TO SAVEPOINT " + strconv.Quote(savepoint))
	if err != nil {
		LogToDB("ERROR", err)
	}
}

// GetChainElements returns all elements for a given chain
func GetChainElements(tx *sqlx.Tx, chains interface{}, chainID int) bool {
	const sqlSelectChains = `
WITH RECURSIVE x
(chain_id, task_id, task_name, script, kind, run_uid, ignore_error, autonomous, database_connection) AS 
(
	SELECT tc.chain_id, tc.task_id, bt.name, 
	bt.script, bt.kind, 
	tc.run_uid, 
	tc.ignore_error, 
	tc.autonomous,
	tc.database_connection 
	FROM timetable.task_chain tc JOIN 
	timetable.base_task bt USING (task_id) 
	WHERE tc.parent_id IS NULL AND tc.chain_id = $1 
	UNION ALL 
	SELECT tc.chain_id, tc.task_id, bt.name, 
	bt.script, bt.kind, 
	tc.run_uid, 
	tc.ignore_error, 
	tc.autonomous,
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
	var err error
	var executor SQLExecutor

	execTx = tx
	if chainElemExec.Autonomous {
		executor = ConfigDb
	} else {
		executor = tx
	}

	//Connect to Remote DB
	if chainElemExec.DatabaseConnection.Valid {
		connectionString := GetConnectionString(chainElemExec.DatabaseConnection)
		remoteDb, execTx, err = GetRemoteDBTransaction(ctx, connectionString)
		if err != nil {
			return err
		}
		if chainElemExec.Autonomous {
			executor = remoteDb
			_ = execTx.Rollback()
		}
		defer FinalizeRemoteDBConnection(remoteDb)
	}

	// Set Role
	if chainElemExec.RunUID.Valid && !chainElemExec.Autonomous {
		SetRole(execTx, chainElemExec.RunUID)
	}

	if chainElemExec.IgnoreError && !chainElemExec.Autonomous {
		mustSavepoint(execTx, chainElemExec.TaskName)
	}

	err = ExecuteSQLCommand(executor, chainElemExec.Script, paramValues)

	if err != nil && chainElemExec.IgnoreError && !chainElemExec.Autonomous {
		mustRollbackToSavepoint(execTx, chainElemExec.TaskName)
	}

	//Reset The Role
	if chainElemExec.RunUID.Valid && !chainElemExec.Autonomous {
		ResetRole(execTx)
	}

	// Commit changes on remote server
	if chainElemExec.DatabaseConnection.Valid && !chainElemExec.Autonomous {
		MustCommitTransaction(execTx)
	}

	return err
}

type SQLExecutor interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
}

// ExecuteSQLCommand executes chain script with parameters inside transaction
func ExecuteSQLCommand(executor SQLExecutor, script string, paramValues []string) error {
	var err error
	var params []interface{}

	if strings.TrimSpace(script) == "" {
		return errors.New("SQL script cannot be empty")
	}
	if len(paramValues) == 0 { //mimic empty param
		_, err = executor.Exec(script)
	} else {
		for _, val := range paramValues {
			if val > "" {
				if err := json.Unmarshal([]byte(val), &params); err != nil {
					return err
				}
				LogToDB("DEBUG", "Executing the command: ", script, fmt.Sprintf("; With parameters: %+v", params))
				_, err = executor.Exec(script, params...)
			}
		}
	}
	return err
}

//GetConnectionString of database_connection
func GetConnectionString(databaseConnection sql.NullString) (connectionString string) {
	err := ConfigDb.Get(&connectionString, "SELECT connect_string "+
		"FROM timetable.database_connection WHERE database_connection = $1", databaseConnection)
	if err != nil {
		LogToDB("ERROR", "Issue while fetching connection string:", err)
	}
	return connectionString
}

//GetRemoteDBTransaction create a remote db connection and returns transaction object
func GetRemoteDBTransaction(ctx context.Context, connectionString string) (*sqlx.DB, *sqlx.Tx, error) {
	if strings.TrimSpace(connectionString) == "" {
		return nil, nil, errors.New("Connection string is blank")
	}
	remoteDb, err := sqlx.ConnectContext(ctx, "postgres", connectionString)
	if err != nil {
		LogToDB("ERROR",
			fmt.Sprintf("Error in remote connection (%s): %v", connectionString, err))
		return nil, nil, err
	}
	LogToDB("LOG", "Remote Connection established...")
	remoteTx, err := remoteDb.BeginTxx(ctx, nil)
	if err != nil {
		LogToDB("ERROR",
			fmt.Sprintf("Error during start of remote transaction (%s): %v", connectionString, err))
		return nil, nil, err
	}
	return remoteDb, remoteTx, nil
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
