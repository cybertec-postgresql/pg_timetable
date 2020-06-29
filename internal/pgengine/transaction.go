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
func MustCommitTransaction(ctx context.Context, tx *sqlx.Tx) {
	LogToDB(ctx, "DEBUG", "Commit transaction for successful chain execution")
	err := tx.Commit()
	if err != nil {
		LogToDB(ctx, "ERROR", "Application cannot commit after job finished: ", err)
	}
}

// MustRollbackTransaction rollbacks transaction and log error in the case of error
func MustRollbackTransaction(ctx context.Context, tx *sqlx.Tx) {
	LogToDB(ctx, "DEBUG", "Rollback transaction for failed chain execution")
	err := tx.Rollback()
	if err != nil {
		LogToDB(ctx, "ERROR", "Application cannot rollback after job failed: ", err)
	}
}

func MustSavepoint(ctx context.Context, tx *sqlx.Tx, savepoint string) {
	LogToDB(ctx, "DEBUG", "Define savepoint to ignore an error for the task: ", strconv.Quote(savepoint))
	_, err := tx.ExecContext(ctx, "SAVEPOINT "+strconv.Quote(savepoint))
	if err != nil {
		LogToDB(ctx, "ERROR", err)
	}
}

func MustRollbackToSavepoint(ctx context.Context, tx *sqlx.Tx, savepoint string) {
	LogToDB(ctx, "DEBUG", "Rollback to savepoint ignoring error for the task: ", savepoint)
	_, err := tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT "+strconv.Quote(savepoint))
	if err != nil {
		LogToDB(ctx, "ERROR", err)
	}
}

// GetChainElements returns all elements for a given chain
func GetChainElements(ctx context.Context, tx *sqlx.Tx, chains interface{}, chainID int) bool {
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

	err := tx.SelectContext(ctx, chains, sqlSelectChains, chainID)

	if err != nil {
		LogToDB(ctx, "ERROR", "Recursive queries to fetch chain tasks failed: ", err)
		return false
	}
	return true
}

// GetChainParamValues returns parameter values to pass for task being executed
func GetChainParamValues(ctx context.Context, tx *sqlx.Tx, paramValues interface{}, chainElemExec *ChainElementExecution) bool {
	const sqlGetParamValues = `
SELECT value
FROM  timetable.chain_execution_parameters
WHERE chain_execution_config = $1
  AND chain_id = $2
ORDER BY order_id ASC`
	err := tx.SelectContext(ctx, paramValues, sqlGetParamValues, chainElemExec.ChainConfig, chainElemExec.ChainID)
	if err != nil {
		LogToDB(ctx, "ERROR", "cannot fetch parameters values for chain: ", err)
		return false
	}
	return true
}

// ExecuteSQLTask executes SQL task
func ExecuteSQLTask(ctx context.Context, tx *sqlx.Tx, chainElemExec *ChainElementExecution, paramValues []string) error {
	var execTx *sqlx.Tx
	var remoteDb *sqlx.DB
	var err error
	var executor sqlx.ExecerContext

	execTx = tx
	if chainElemExec.Autonomous {
		executor = ConfigDb
	} else {
		executor = tx
	}

	//Connect to Remote DB
	if chainElemExec.DatabaseConnection.Valid {
		connectionString := GetConnectionString(ctx, chainElemExec.DatabaseConnection)
		remoteDb, execTx, err = GetRemoteDBTransaction(ctx, connectionString)
		if err != nil {
			return err
		}
		if chainElemExec.Autonomous {
			executor = remoteDb
			_ = execTx.Rollback()
		}
		defer FinalizeRemoteDBConnection(ctx, remoteDb)
	}

	// Set Role
	if chainElemExec.RunUID.Valid && !chainElemExec.Autonomous {
		SetRole(ctx, execTx, chainElemExec.RunUID)
	}

	if chainElemExec.IgnoreError && !chainElemExec.Autonomous {
		MustSavepoint(ctx, execTx, chainElemExec.TaskName)
	}

	err = ExecuteSQLCommand(ctx, executor, chainElemExec.Script, paramValues)

	if err != nil && chainElemExec.IgnoreError && !chainElemExec.Autonomous {
		MustRollbackToSavepoint(ctx, execTx, chainElemExec.TaskName)
	}

	//Reset The Role
	if chainElemExec.RunUID.Valid && !chainElemExec.Autonomous {
		ResetRole(ctx, execTx)
	}

	// Commit changes on remote server
	if chainElemExec.DatabaseConnection.Valid && !chainElemExec.Autonomous {
		MustCommitTransaction(ctx, execTx)
	}

	return err
}

// ExecuteSQLCommand executes chain script with parameters inside transaction
func ExecuteSQLCommand(ctx context.Context, executor sqlx.ExecerContext, script string, paramValues []string) error {
	var err error
	var params []interface{}

	if strings.TrimSpace(script) == "" {
		return errors.New("SQL script cannot be empty")
	}
	if len(paramValues) == 0 { //mimic empty param
		_, err = executor.ExecContext(ctx, script)
	} else {
		for _, val := range paramValues {
			if val > "" {
				if err := json.Unmarshal([]byte(val), &params); err != nil {
					return err
				}
				LogToDB(ctx, "DEBUG", "Executing the command: ", script, fmt.Sprintf("; With parameters: %+v", params))
				_, err = executor.ExecContext(ctx, script, params...)
			}
		}
	}
	return err
}

//GetConnectionString of database_connection
func GetConnectionString(ctx context.Context, databaseConnection sql.NullString) (connectionString string) {
	err := ConfigDb.Get(&connectionString, "SELECT connect_string "+
		"FROM timetable.database_connection WHERE database_connection = $1", databaseConnection)
	if err != nil {
		LogToDB(ctx, "ERROR", "Issue while fetching connection string:", err)
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
		LogToDB(ctx, "ERROR",
			fmt.Sprintf("Error in remote connection (%s): %v", connectionString, err))
		return nil, nil, err
	}
	LogToDB(ctx, "LOG", "Remote Connection established...")
	remoteTx, err := remoteDb.BeginTxx(ctx, nil)
	if err != nil {
		LogToDB(ctx, "ERROR",
			fmt.Sprintf("Error during start of remote transaction (%s): %v", connectionString, err))
		return nil, nil, err
	}
	return remoteDb, remoteTx, nil
}

// FinalizeRemoteDBConnection closes session
func FinalizeRemoteDBConnection(ctx context.Context, remoteDb *sqlx.DB) {
	LogToDB(ctx, "LOG", "Closing remote session")
	if err := remoteDb.Close(); err != nil {
		LogToDB(ctx, "ERROR", "Cannot close database connection:", err)
	}
	remoteDb = nil
}

// SetRole - set the current user identifier of the current session
func SetRole(ctx context.Context, tx *sqlx.Tx, runUID sql.NullString) {
	LogToDB(ctx, "LOG", "Setting Role to ", runUID.String)
	_, err := tx.Exec(fmt.Sprintf("SET ROLE %v", runUID.String))
	if err != nil {
		LogToDB(ctx, "ERROR", "Error in Setting role", err)
	}
}

//ResetRole - RESET forms reset the current user identifier to be the current session user identifier
func ResetRole(ctx context.Context, tx *sqlx.Tx) {
	LogToDB(ctx, "LOG", "Resetting Role")
	const sqlResetRole = `RESET ROLE`
	_, err := tx.Exec(sqlResetRole)
	if err != nil {
		LogToDB(ctx, "ERROR", "Error in ReSetting role", err)
	}
}
