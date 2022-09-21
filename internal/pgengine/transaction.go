package pgengine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/internal/log"
	pgx "github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// Chain structure used to represent tasks chains
type Chain struct {
	ChainID            int    `db:"chain_id"`
	ChainName          string `db:"chain_name"`
	SelfDestruct       bool   `db:"self_destruct"`
	ExclusiveExecution bool   `db:"exclusive_execution"`
	MaxInstances       int    `db:"max_instances"`
	Timeout            int    `db:"timeout"`
}

// IntervalChain structure used to represent repeated chains.
type IntervalChain struct {
	Chain
	Interval    int  `db:"interval_seconds"`
	RepeatAfter bool `db:"repeat_after"`
}

func (ichain IntervalChain) IsListed(ichains []IntervalChain) bool {
	for _, ic := range ichains {
		if ichain.ChainID == ic.ChainID {
			return true
		}
	}
	return false
}

// ChainTask structure describes each chain task
type ChainTask struct {
	ChainID       int         `db:"-"`
	TaskID        int         `db:"task_id"`
	Script        string      `db:"command"`
	Kind          string      `db:"kind"`
	RunAs         pgtype.Text `db:"run_as"`
	IgnoreError   bool        `db:"ignore_error"`
	Autonomous    bool        `db:"autonomous"`
	ConnectString pgtype.Text `db:"database_connection"`
	Timeout       int         `db:"timeout"` // in milliseconds
	StartedAt     time.Time   `db:"-"`
	Duration      int64       `db:"-"` // in microseconds
	Txid          int         `db:"-"`
}

// StartTransaction returns transaction object, transaction id and error
func (pge *PgEngine) StartTransaction(ctx context.Context, chainID int) (tx pgx.Tx, txid int, err error) {
	tx, err = pge.ConfigDb.Begin(ctx)
	if err != nil {
		return
	}
	err = tx.QueryRow(ctx, "SELECT txid_current()").Scan(&txid)
	if err != nil {
		return
	}
	_, err = tx.Exec(ctx, `SELECT set_config('pg_timetable.current_chain_id', $1, true)`, strconv.Itoa(chainID))
	return
}

// CommitTransaction commits transaction and log error in the case of error
func (pge *PgEngine) CommitTransaction(ctx context.Context, tx pgx.Tx) {
	err := tx.Commit(ctx)
	if err != nil {
		log.GetLogger(ctx).WithError(err).Error("Application cannot commit after job finished")
	}
}

// RollbackTransaction rollbacks transaction and log error in the case of error
func (pge *PgEngine) RollbackTransaction(ctx context.Context, tx pgx.Tx) {
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
func (pge *PgEngine) GetChainElements(ctx context.Context, tx pgx.Tx, chainTasks *[]ChainTask, chainID int) error {
	const sqlSelectChainTasks = `SELECT task_id, command, kind, run_as, ignore_error, autonomous, database_connection, timeout
FROM timetable.task WHERE chain_id = $1 ORDER BY task_order ASC`
	// return Select(ctx, tx, chainTasks, sqlSelectChainTasks, chainID)
	rows, err := pge.ConfigDb.Query(ctx, sqlSelectChainTasks, chainID)
	if err != nil {
		return err
	}
	*chainTasks, err = pgx.CollectRows(rows, RowToStructByName[ChainTask])
	return err
}

// GetChainParamValues returns parameter values to pass for task being executed
func (pge *PgEngine) GetChainParamValues(ctx context.Context, tx pgx.Tx, paramValues *[]string, task *ChainTask) error {
	const sqlGetParamValues = `SELECT value FROM timetable.parameter WHERE task_id = $1 AND value IS NOT NULL ORDER BY order_id ASC`
	// return Select(ctx, tx, paramValues, sqlGetParamValues, task.TaskID)
	rows, err := pge.ConfigDb.Query(ctx, sqlGetParamValues, task.TaskID)
	if err != nil {
		return err
	}
	*paramValues, err = pgx.CollectRows(rows, pgx.RowTo[string])
	return err
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
	if task.ConnectString.Valid {
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

	if !task.Autonomous {
		pge.SetRole(ctx, execTx, task.RunAs)
		if task.IgnoreError {
			pge.MustSavepoint(ctx, execTx, fmt.Sprintf("task_%d", task.TaskID))
		}
	}

	pge.SetCurrentTaskContext(ctx, execTx, task.TaskID)
	out, err = pge.ExecuteSQLCommand(ctx, executor, task.Script, paramValues)

	if err != nil && task.IgnoreError && !task.Autonomous {
		pge.MustRollbackToSavepoint(ctx, execTx, fmt.Sprintf("task_%d", task.TaskID))
	}

	//Reset The Role
	if task.RunAs.Valid && !task.Autonomous {
		pge.ResetRole(ctx, execTx)
	}

	// Commit changes on remote server
	if task.ConnectString.Valid && !task.Autonomous {
		pge.CommitTransaction(ctx, execTx)
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
		out = ct.String()
	} else {
		for _, val := range paramValues {
			if val > "" {
				if err = json.Unmarshal([]byte(val), &params); err != nil {
					return
				}
				ct, err = executor.Exec(ctx, command, params...)
				out = out + ct.String() + "\n"
			}
		}
	}
	return
}

// GetRemoteDBTransaction create a remote db connection and returns transaction object
func (pge *PgEngine) GetRemoteDBTransaction(ctx context.Context, connectionString string) (PgxConnIface, pgx.Tx, error) {
	if strings.TrimSpace(connectionString) == "" {
		return nil, nil, errors.New("Connection string is blank")
	}
	connConfig, err := pgx.ParseConfig(connectionString)
	if err != nil {
		return nil, nil, err
	}
	// connConfig.Logger = log.NewPgxLogger(pge.l)
	// if pge.Verbose() {
	// 	connConfig.LogLevel = pgx.LogLevelDebug
	// } else {
	// 	connConfig.LogLevel = pgx.LogLevelWarn
	// }
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
func (pge *PgEngine) SetRole(ctx context.Context, tx pgx.Tx, runUID pgtype.Text) {
	if !runUID.Valid {
		return
	}
	l := log.GetLogger(ctx)
	l.Info("Setting Role to ", runUID.String)
	_, err := tx.Exec(ctx, fmt.Sprintf("SET ROLE %v", runUID.String))
	if err != nil {
		l.WithError(err).Error("Error in Setting role", err)
	}
}

// ResetRole - RESET forms reset the current user identifier to be the current session user identifier
func (pge *PgEngine) ResetRole(ctx context.Context, tx pgx.Tx) {
	l := log.GetLogger(ctx)
	l.Info("Resetting Role")
	const sqlResetRole = `RESET ROLE`
	_, err := tx.Exec(ctx, sqlResetRole)
	if err != nil {
		l.WithError(err).Error("Failed to set a role", err)
	}
}

// SetCurrentTaskContext - set the working transaction "pg_timetable.current_task_id" run-time parameter
func (pge *PgEngine) SetCurrentTaskContext(ctx context.Context, tx pgx.Tx, taskID int) {
	l := log.GetLogger(ctx)
	l.Debug("Setting current task context to ", taskID)
	_, err := tx.Exec(ctx, "SELECT set_config('pg_timetable.current_task_id', $1, true)", strconv.Itoa(taskID))
	if err != nil {
		l.WithError(err).Error("Failed to set current task context", err)
	}
}
