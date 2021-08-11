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

// ChainTask structure describes each chain task
type ChainTask struct {
	ChainID       int
	TaskID        int            `db:"task_id"`
	Script        string         `db:"command"`
	Kind          string         `db:"kind"`
	RunAs         pgtype.Varchar `db:"run_as"`
	IgnoreError   bool           `db:"ignore_error"`
	Autonomous    bool           `db:"autonomous"`
	ConnectString pgtype.Varchar `db:"connect_string"`
	Timeout       int            `db:"timeout"` // in milliseconds
	StartedAt     time.Time
	Duration      int64 // in microseconds
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
func (pge *PgEngine) GetChainElements(ctx context.Context, tx pgx.Tx, chains interface{}, taskID int) bool {
	const sqlSelectChainTasks = `WITH RECURSIVE x
(task_id, command, kind, run_as, ignore_error, autonomous, connect_string, timeout) 
AS (
	SELECT tc.task_id, tc.command, tc.kind, tc.run_as, tc.ignore_error, tc.autonomous, tc.database_connection, tc.timeout 
	FROM timetable.task tc
	WHERE tc.parent_id IS NULL AND tc.task_id = $1 
UNION ALL 
	SELECT tc.task_id, tc.command, tc.kind, tc.run_as, tc.ignore_error, tc.autonomous, tc.database_connection, tc.timeout 
	FROM timetable.task tc JOIN x ON (x.task_id = tc.parent_id) 
) 
	SELECT * FROM x`
	err := pgxscan.Select(ctx, tx, chains, sqlSelectChainTasks, taskID)
	if err != nil {
		log.GetLogger(ctx).WithError(err).Error("Failed to retrieve chain elements")
		return false
	}
	return true
}

// GetChainParamValues returns parameter values to pass for task being executed
func (pge *PgEngine) GetChainParamValues(ctx context.Context, tx pgx.Tx, paramValues interface{}, task *ChainTask) bool {
	const sqlGetParamValues = `SELECT value FROM timetable.parameter WHERE chain_id = $1 AND task_id = $2 AND value IS NOT NULL ORDER BY order_id ASC`
	err := pgxscan.Select(ctx, tx, paramValues, sqlGetParamValues, task.ChainID, task.TaskID)
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
func (pge *PgEngine) ExecuteSQLTask(ctx context.Context, tx pgx.Tx, task *ChainTask, paramValues []string) (out string, err error) {
	var execTx pgx.Tx
	var remoteDb PgxConnIface
	var executor executor

	execTx = tx
	if task.Autonomous {
		executor = pge.ConfigDb
	} else {
		executor = tx
	}

	//Connect to Remote DB
	if task.ConnectString.Status != pgtype.Null {
		remoteDb, execTx, err = pge.GetRemoteDBTransaction(ctx, task.ConnectString.String)
		if err != nil {
			return
		}
		if task.Autonomous {
			executor = remoteDb
			_ = execTx.Rollback(ctx)
		} else {
			executor = execTx
		}

		defer pge.FinalizeRemoteDBConnection(ctx, remoteDb)
	}

	// Set Role
	if task.RunAs.Status != pgtype.Null && !task.Autonomous {
		pge.SetRole(ctx, execTx, task.RunAs)
	}

	if task.IgnoreError && !task.Autonomous {
		pge.MustSavepoint(ctx, execTx, fmt.Sprintf("task_%d", task.TaskID))
	}

	out, err = pge.ExecuteSQLCommand(ctx, executor, task.Script, paramValues)

	if err != nil && task.IgnoreError && !task.Autonomous {
		pge.MustRollbackToSavepoint(ctx, execTx, fmt.Sprintf("task_%d", task.TaskID))
	}

	//Reset The Role
	if task.RunAs.Status != pgtype.Null && !task.Autonomous {
		pge.ResetRole(ctx, execTx)
	}

	// Commit changes on remote server
	if task.ConnectString.Status != pgtype.Null && !task.Autonomous {
		pge.MustCommitTransaction(ctx, execTx)
	}

	return
}

// ExecuteSQLCommand executes chain command with parameters inside transaction
func (pge *PgEngine) ExecuteSQLCommand(ctx context.Context, executor executor, command string, paramValues []string) (out string, err error) {
	var ct pgconn.CommandTag
	var params []interface{}

	if strings.TrimSpace(command) == "" {
		return "", errors.New("SQL command cannot be empty")
	}
	if len(paramValues) == 0 { //mimic empty param
		ct, err = executor.Exec(ctx, command)
		out = string(ct)
	} else {
		for _, val := range paramValues {
			if val > "" {
				if err = json.Unmarshal([]byte(val), &params); err != nil {
					return
				}
				ct, err = executor.Exec(ctx, command, params...)
				out = out + string(ct) + "\n"
			}
		}
	}
	return
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
	if pge.Verbose() {
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
