package pgengine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/internal/log"
	"github.com/georgysavva/scany/pgxscan"
	"github.com/jackc/pgconn"
	"github.com/jackc/pgtype"
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
	RunUID             pgtype.Varchar `db:"run_uid"`
	IgnoreError        bool           `db:"ignore_error"`
	Autonomous         bool           `db:"autonomous"`
	DatabaseConnection pgtype.Varchar `db:"database_connection"`
	ConnectString      pgtype.Varchar `db:"connect_string"`
	StartedAt          time.Time
	Duration           int64 // in microseconds
}

func (chainElem ChainElementExecution) String() string {
	data, _ := json.Marshal(chainElem)
	return string(data)
}

// StartTransaction return transaction object and panic in the case of error
func (pge *PgEngine) StartTransaction(ctx context.Context) (pgx.Tx, error) {
	return pge.ConfigDb.Begin(ctx)
}

// MustCommitTransaction commits transaction and log error in the case of error
func (pge *PgEngine) MustCommitTransaction(ctx context.Context, tx pgx.Tx) {
	err := tx.Commit(ctx)
	if err != nil {
		log.GetLogger(ctx).WithError(err).Error("Application cannot commit after job finished")
	}
}

// MustRollbackTransaction rollbacks transaction and log error in the case of error
func (pge *PgEngine) MustRollbackTransaction(ctx context.Context, tx pgx.Tx) {
	err := tx.Rollback(ctx)
	if err != nil {
		log.GetLogger(ctx).WithError(err).Error("Application cannot rollback after job failed")
	}
}

func quoteIdent(s string) string {
	return `"` + strings.Replace(s, `"`, `""`, -1) + `"`
}

// MustSavepoint creates SAVDEPOINT in transaction and log error in the case of error
func (pge *PgEngine) MustSavepoint(ctx context.Context, tx pgx.Tx, savepoint string) {
	_, err := tx.Exec(ctx, "SAVEPOINT "+quoteIdent(savepoint))
	if err != nil {
		log.GetLogger(ctx).WithError(err).Error("Savepoint failed")
	}
}

// MustRollbackToSavepoint rollbacks transaction to SAVEPOINT and log error in the case of error
func (pge *PgEngine) MustRollbackToSavepoint(ctx context.Context, tx pgx.Tx, savepoint string) {
	_, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT "+quoteIdent(savepoint))
	if err != nil {
		log.GetLogger(ctx).WithError(err).Error("Rollback to savepoint failed")
	}
}

// GetChainElements returns all elements for a given chain
func (pge *PgEngine) GetChainElements(ctx context.Context, tx pgx.Tx, chains interface{}, chainID int) bool {
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
		log.GetLogger(ctx).WithError(err).Error("Failed to retrieve chain elements")
		return false
	}
	return true
}

// GetChainParamValues returns parameter values to pass for task being executed
func (pge *PgEngine) GetChainParamValues(ctx context.Context, tx pgx.Tx, paramValues interface{}, chainElemExec *ChainElementExecution) bool {
	const sqlGetParamValues = `
SELECT value
FROM  timetable.chain_execution_parameters
WHERE chain_execution_config = $1
  AND chain_id = $2
ORDER BY order_id ASC`
	err := pgxscan.Select(ctx, tx, paramValues, sqlGetParamValues, chainElemExec.ChainConfig, chainElemExec.ChainID)
	if err != nil {
		log.GetLogger(ctx).WithError(err).Error("cannot fetch parameters values for chain: ", err)
		return false
	}
	return true
}

type executor interface {
	Exec(ctx context.Context, sql string, arguments ...interface{}) (commandTag pgconn.CommandTag, err error)
}

// ExecuteSQLTask executes SQL task
func (pge *PgEngine) ExecuteSQLTask(ctx context.Context, tx pgx.Tx, chainElemExec *ChainElementExecution, paramValues []string) (out string, err error) {
	var execTx pgx.Tx
	var remoteDb PgxConnIface
	var executor executor

	execTx = tx
	if chainElemExec.Autonomous {
		executor = pge.ConfigDb
	} else {
		executor = tx
	}

	//Connect to Remote DB
	if chainElemExec.DatabaseConnection.Status != pgtype.Null {
		connectionString := pge.GetConnectionString(ctx, chainElemExec.DatabaseConnection)
		remoteDb, execTx, err = pge.GetRemoteDBTransaction(ctx, connectionString)
		if err != nil {
			return
		}
		if chainElemExec.Autonomous {
			executor = remoteDb
			_ = execTx.Rollback(ctx)
		} else {
			executor = execTx
		}

		defer pge.FinalizeRemoteDBConnection(ctx, remoteDb)
	}

	// Set Role
	if chainElemExec.RunUID.Status != pgtype.Null && !chainElemExec.Autonomous {
		pge.SetRole(ctx, execTx, chainElemExec.RunUID)
	}

	if chainElemExec.IgnoreError && !chainElemExec.Autonomous {
		pge.MustSavepoint(ctx, execTx, chainElemExec.TaskName)
	}

	out, err = pge.ExecuteSQLCommand(ctx, executor, chainElemExec.Script, paramValues)

	if err != nil && chainElemExec.IgnoreError && !chainElemExec.Autonomous {
		pge.MustRollbackToSavepoint(ctx, execTx, chainElemExec.TaskName)
	}

	//Reset The Role
	if chainElemExec.RunUID.Status != pgtype.Null && !chainElemExec.Autonomous {
		pge.ResetRole(ctx, execTx)
	}

	// Commit changes on remote server
	if chainElemExec.DatabaseConnection.Status != pgtype.Null && !chainElemExec.Autonomous {
		pge.MustCommitTransaction(ctx, execTx)
	}

	return
}

// ExecuteSQLCommand executes chain script with parameters inside transaction
func (pge *PgEngine) ExecuteSQLCommand(ctx context.Context, executor executor, script string, paramValues []string) (out string, err error) {
	var ct pgconn.CommandTag
	var params []interface{}

	if strings.TrimSpace(script) == "" {
		return "", errors.New("SQL script cannot be empty")
	}
	if len(paramValues) == 0 { //mimic empty param
		ct, err = executor.Exec(ctx, script)
		out = string(ct)
	} else {
		for _, val := range paramValues {
			if val > "" {
				if err = json.Unmarshal([]byte(val), &params); err != nil {
					return
				}
				ct, err = executor.Exec(ctx, script, params...)
				out = out + string(ct) + "\n"
			}
		}
	}
	return
}

//GetConnectionString of database_connection
func (pge *PgEngine) GetConnectionString(ctx context.Context, databaseConnection pgtype.Varchar) (connectionString string) {
	err := pge.ConfigDb.QueryRow(ctx, "SELECT connect_string "+
		"FROM timetable.database_connection WHERE database_connection = $1", databaseConnection).Scan(&connectionString)
	if err != nil {
		log.GetLogger(ctx).WithError(err).Error("Issue while fetching connection string:", err)
	}
	return connectionString
}

//GetRemoteDBTransaction create a remote db connection and returns transaction object
func (pge *PgEngine) GetRemoteDBTransaction(ctx context.Context, connectionString string) (PgxConnIface, pgx.Tx, error) {
	if strings.TrimSpace(connectionString) == "" {
		return nil, nil, errors.New("Connection string is blank")
	}
	connConfig, err := pgx.ParseConfig(connectionString)
	if err != nil {
		return nil, nil, err
	}
	connConfig.Logger = log.NewPgxLogger(pge.l)
	if pge.Verbose {
		connConfig.LogLevel = pgx.LogLevelDebug
	} else {
		connConfig.LogLevel = pgx.LogLevelWarn
	}
	l := log.GetLogger(ctx)
	remoteDb, err := pgx.ConnectConfig(ctx, connConfig)
	if err != nil {
		l.WithError(err).Error("Failed to establish remote connection")
		return nil, nil, err
	}
	l.Info("Remote connection established...")
	remoteTx, err := remoteDb.Begin(ctx)
	if err != nil {
		l.WithError(err).Error("Failed to start remote transaction")
		return nil, nil, err
	}
	return remoteDb, remoteTx, nil
}

// FinalizeRemoteDBConnection closes session
func (pge *PgEngine) FinalizeRemoteDBConnection(ctx context.Context, remoteDb PgxConnIface) {
	l := log.GetLogger(ctx)
	l.Info("Closing remote session")
	if err := remoteDb.Close(ctx); err != nil {
		l.WithError(err).Error("Cannot close database connection:", err)
	}
	remoteDb = nil
}

// SetRole - set the current user identifier of the current session
func (pge *PgEngine) SetRole(ctx context.Context, tx pgx.Tx, runUID pgtype.Varchar) {
	l := log.GetLogger(ctx)
	l.Info("Setting Role to ", runUID.String)
	_, err := tx.Exec(ctx, fmt.Sprintf("SET ROLE %v", runUID.String))
	if err != nil {
		l.WithError(err).Error("Error in Setting role", err)
	}
}

//ResetRole - RESET forms reset the current user identifier to be the current session user identifier
func (pge *PgEngine) ResetRole(ctx context.Context, tx pgx.Tx) {
	l := log.GetLogger(ctx)
	l.Info("Resetting Role")
	const sqlResetRole = `RESET ROLE`
	_, err := tx.Exec(ctx, sqlResetRole)
	if err != nil {
		l.WithError(err).Error("Error in ReSetting role", err)
	}
}
