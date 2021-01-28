package pgengine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/georgysavva/scany/pgxscan"
	"github.com/jackc/pgconn"
	pgx "github.com/jackc/pgx/v4"
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
func StartTransaction(ctx context.Context) (pgx.Tx, error) {
	return ConfigDb.Begin(ctx)
}

// MustCommitTransaction commits transaction and log error in the case of error
func MustCommitTransaction(ctx context.Context, tx pgx.Tx) {
	LogToDB(ctx, "DEBUG", "Commit transaction for successful chain execution")
	err := tx.Commit(ctx)
	if err != nil {
		LogToDB(ctx, "ERROR", "Application cannot commit after job finished: ", err)
	}
}

// MustRollbackTransaction rollbacks transaction and log error in the case of error
func MustRollbackTransaction(ctx context.Context, tx pgx.Tx) {
	LogToDB(ctx, "DEBUG", "Rollback transaction for failed chain execution")
	err := tx.Rollback(ctx)
	if err != nil {
		LogToDB(ctx, "ERROR", "Application cannot rollback after job failed: ", err)
	}
}

func quoteIdent(s string) string {
	return `"` + strings.Replace(s, `"`, `""`, -1) + `"`
}

// MustSavepoint creates SAVDEPOINT in transaction and log error in the case of error
func MustSavepoint(ctx context.Context, tx pgx.Tx, savepoint string) {
	LogToDB(ctx, "DEBUG", "Define savepoint to ignore an error for the task: ", quoteIdent(savepoint))
	_, err := tx.Exec(ctx, "SAVEPOINT "+quoteIdent(savepoint))
	if err != nil {
		LogToDB(ctx, "ERROR", err)
	}
}

// MustRollbackToSavepoint rollbacks transaction to SAVEPOINT and log error in the case of error
func MustRollbackToSavepoint(ctx context.Context, tx pgx.Tx, savepoint string) {
	LogToDB(ctx, "DEBUG", "Rollback to savepoint ignoring error for the task: ", quoteIdent(savepoint))
	_, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT "+quoteIdent(savepoint))
	if err != nil {
		LogToDB(ctx, "ERROR", err)
	}
}

// GetChainElements returns all elements for a given chain
func GetChainElements(ctx context.Context, tx pgx.Tx, chains interface{}, chainID int) bool {
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

	err := pgxscan.Select(ctx, tx, chains, sqlSelectChains, chainID)

	if err != nil {
		LogToDB(ctx, "ERROR", "Recursive queries to fetch chain tasks failed: ", err)
		return false
	}
	return true
}

// GetChainParamValues returns parameter values to pass for task being executed
func GetChainParamValues(ctx context.Context, tx pgx.Tx, paramValues interface{}, chainElemExec *ChainElementExecution) bool {
	const sqlGetParamValues = `
SELECT value
FROM  timetable.chain_execution_parameters
WHERE chain_execution_config = $1
  AND chain_id = $2
ORDER BY order_id ASC`
	err := pgxscan.Select(ctx, tx, paramValues, sqlGetParamValues, chainElemExec.ChainConfig, chainElemExec.ChainID)
	if err != nil {
		LogToDB(ctx, "ERROR", "cannot fetch parameters values for chain: ", err)
		return false
	}
	return true
}

type executor interface {
	Exec(ctx context.Context, sql string, arguments ...interface{}) (commandTag pgconn.CommandTag, err error)
}

// ExecuteSQLTask executes SQL task
func ExecuteSQLTask(ctx context.Context, tx pgx.Tx, chainElemExec *ChainElementExecution, paramValues []string) error {
	var execTx pgx.Tx
	var remoteDb PgxConnIface
	var err error
	var executor executor

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
			_ = execTx.Rollback(ctx)
		} else {
			executor = execTx
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
func ExecuteSQLCommand(ctx context.Context, executor executor, script string, paramValues []string) error {
	var err error
	var params []interface{}

	if strings.TrimSpace(script) == "" {
		return errors.New("SQL script cannot be empty")
	}
	if len(paramValues) == 0 { //mimic empty param
		_, err = executor.Exec(ctx, script)
	} else {
		for _, val := range paramValues {
			if val > "" {
				if err := json.Unmarshal([]byte(val), &params); err != nil {
					return err
				}
				LogToDB(ctx, "DEBUG", "Executing the command: ", script, fmt.Sprintf("; With parameters: %+v", params))
				_, err = executor.Exec(ctx, script, params...)
			}
		}
	}
	return err
}

//GetConnectionString of database_connection
func GetConnectionString(ctx context.Context, databaseConnection sql.NullString) (connectionString string) {
	err := ConfigDb.QueryRow(ctx, "SELECT connect_string "+
		"FROM timetable.database_connection WHERE database_connection = $1", databaseConnection).Scan(&connectionString)
	if err != nil {
		LogToDB(ctx, "ERROR", "Issue while fetching connection string:", err)
	}
	return connectionString
}

//GetRemoteDBTransaction create a remote db connection and returns transaction object
func GetRemoteDBTransaction(ctx context.Context, connectionString string) (PgxConnIface, pgx.Tx, error) {
	if strings.TrimSpace(connectionString) == "" {
		return nil, nil, errors.New("Connection string is blank")
	}
	remoteDb, err := pgx.Connect(ctx, connectionString)
	if err != nil {
		LogToDB(ctx, "ERROR",
			fmt.Sprintf("Error in remote connection (%s): %v", connectionString, err))
		return nil, nil, err
	}
	LogToDB(ctx, "LOG", "Remote Connection established...")
	remoteTx, err := remoteDb.Begin(ctx)
	if err != nil {
		LogToDB(ctx, "ERROR",
			fmt.Sprintf("Error during start of remote transaction (%s): %v", connectionString, err))
		return nil, nil, err
	}
	return remoteDb, remoteTx, nil
}

// FinalizeRemoteDBConnection closes session
func FinalizeRemoteDBConnection(ctx context.Context, remoteDb PgxConnIface) {
	LogToDB(ctx, "LOG", "Closing remote session")
	if err := remoteDb.Close(ctx); err != nil {
		LogToDB(ctx, "ERROR", "Cannot close database connection:", err)
	}
	remoteDb = nil
}

// SetRole - set the current user identifier of the current session
func SetRole(ctx context.Context, tx pgx.Tx, runUID sql.NullString) {
	LogToDB(ctx, "LOG", "Setting Role to ", runUID.String)
	_, err := tx.Exec(ctx, fmt.Sprintf("SET ROLE %v", runUID.String))
	if err != nil {
		LogToDB(ctx, "ERROR", "Error in Setting role", err)
	}
}

//ResetRole - RESET forms reset the current user identifier to be the current session user identifier
func ResetRole(ctx context.Context, tx pgx.Tx) {
	LogToDB(ctx, "LOG", "Resetting Role")
	const sqlResetRole = `RESET ROLE`
	_, err := tx.Exec(ctx, sqlResetRole)
	if err != nil {
		LogToDB(ctx, "ERROR", "Error in ReSetting role", err)
	}
}
