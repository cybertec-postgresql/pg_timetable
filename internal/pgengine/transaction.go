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
	ChainID            int         `db:"chain_id"`
	ChainName          string      `db:"chain_name"`
	SelfDestruct       bool        `db:"self_destruct"`
	ExclusiveExecution bool        `db:"exclusive_execution"`
	MaxInstances       int         `db:"max_instances"`
	Timeout            int         `db:"timeout"`
	OnErrorSQL         pgtype.Text `db:"on_error"`
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
	Txid          int64       `db:"-"`
}

func (task *ChainTask) IsRemote() bool {
	return task.ConnectString.Valid && strings.TrimSpace(task.ConnectString.String) != ""
}

// StartTransaction returns transaction object, transaction id and error
func (pge *PgEngine) StartTransaction(ctx context.Context) (tx pgx.Tx, txid int64, err error) {
	tx, err = pge.ConfigDb.Begin(ctx)
	if err != nil {
		return
	}
	err = tx.QueryRow(ctx, "SELECT txid_current()").Scan(&txid)
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
func (pge *PgEngine) MustSavepoint(ctx context.Context, tx pgx.Tx, taskID int) {
	if _, err := tx.Exec(ctx, fmt.Sprintf("SAVEPOINT task_%d", taskID)); err != nil {
		log.GetLogger(ctx).WithError(err).Error("Savepoint failed")
	}
}

// MustRollbackToSavepoint rollbacks transaction to SAVEPOINT and log error in the case of error
func (pge *PgEngine) MustRollbackToSavepoint(ctx context.Context, tx pgx.Tx, taskID int) {
	if _, err := tx.Exec(ctx, fmt.Sprintf("ROLLBACK TO SAVEPOINT task_%d", taskID)); err != nil {
		log.GetLogger(ctx).WithError(err).Error("Rollback to savepoint failed")
	}
}

// GetChainElements returns all elements for a given chain
func (pge *PgEngine) GetChainElements(ctx context.Context, chainTasks *[]ChainTask, chainID int) error {
	const sqlSelectChainTasks = `SELECT task_id, command, kind, run_as, ignore_error, autonomous, database_connection, timeout
FROM timetable.task WHERE chain_id = $1 ORDER BY task_order ASC`
	rows, err := pge.ConfigDb.Query(ctx, sqlSelectChainTasks, chainID)
	if err != nil {
		return err
	}
	*chainTasks, err = pgx.CollectRows(rows, pgx.RowToStructByName[ChainTask])
	return err
}

// GetChainParamValues returns parameter values to pass for task being executed
func (pge *PgEngine) GetChainParamValues(ctx context.Context, paramValues *[]string, task *ChainTask) error {
	const sqlGetParamValues = `SELECT value FROM timetable.parameter WHERE task_id = $1 AND value IS NOT NULL ORDER BY order_id ASC`
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

// execRemoteSQLTask executes task against remote connection
func (pge *PgEngine) execRemoteSQLTask(ctx context.Context, task *ChainTask, paramValues []string) (string, error) {
	remoteDb, err := pge.GetRemoteDBConnection(ctx, task.ConnectString.String)
	if err != nil {
		return "", err
	}
	defer pge.FinalizeRemoteDBConnection(ctx, remoteDb)
	if err := pge.SetRole(ctx, remoteDb, task.RunAs); err != nil {
		return "", err
	}
	pge.SetCurrentTaskContext(ctx, remoteDb, task.ChainID, task.TaskID)
	return pge.ExecuteSQLCommand(ctx, remoteDb, task.Script, paramValues)
}

// execAutonomousSQLTask executes autonomous task in an acquired connection from pool
func (pge *PgEngine) execAutonomousSQLTask(ctx context.Context, task *ChainTask, paramValues []string) (string, error) {
	conn, err := pge.ConfigDb.Acquire(ctx)
	if err != nil {
		return "", err
	}
	defer conn.Release()
	if err := pge.SetRole(ctx, conn, task.RunAs); err != nil {
		return "", err
	}
	pge.SetCurrentTaskContext(ctx, conn, task.ChainID, task.TaskID)
	return pge.ExecuteSQLCommand(ctx, conn, task.Script, paramValues)
}

// execAutonomousSQLTask executes autonomous task in an acquired connection from pool
func (pge *PgEngine) execLocalSQLTask(ctx context.Context, tx pgx.Tx, task *ChainTask, paramValues []string) (out string, err error) {
	if err := pge.SetRole(ctx, tx, task.RunAs); err != nil {
		return "", err
	}
	if task.IgnoreError {
		pge.MustSavepoint(ctx, tx, task.TaskID)
	}
	pge.SetCurrentTaskContext(ctx, tx, task.ChainID, task.TaskID)
	out, err = pge.ExecuteSQLCommand(ctx, tx, task.Script, paramValues)
	if err != nil && task.IgnoreError {
		pge.MustRollbackToSavepoint(ctx, tx, task.TaskID)
	}
	if task.RunAs.Valid {
		pge.ResetRole(ctx, tx)
	}
	return
}

// ExecuteSQLTask executes SQL task
func (pge *PgEngine) ExecuteSQLTask(ctx context.Context, tx pgx.Tx, task *ChainTask, paramValues []string) (out string, err error) {
	switch {
	case task.IsRemote():
		return pge.execRemoteSQLTask(ctx, task, paramValues)
	case task.Autonomous:
		return pge.execAutonomousSQLTask(ctx, task, paramValues)
	default:
		return pge.execLocalSQLTask(ctx, tx, task, paramValues)
	}
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

// GetRemoteDBConnection create a remote db connection and returns transaction object
func (pge *PgEngine) GetRemoteDBConnection(ctx context.Context, connectionString string) (conn PgxConnIface, err error) {
	var connConfig *pgx.ConnConfig
	if connConfig, err = pgx.ParseConfig(connectionString); err != nil {
		return nil, err
	}
	l := log.GetLogger(ctx)
	conn, err = pgx.ConnectConfig(ctx, connConfig)
	if err != nil {
		return nil, err
	}
	l.Info("Remote connection established...")
	return
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
func (pge *PgEngine) SetRole(ctx context.Context, executor executor, runUID pgtype.Text) error {
	if !runUID.Valid {
		return errors.New("Empty Run As value")
	}
	l := log.GetLogger(ctx)
	l.Info("Setting Role to ", runUID.String)
	_, err := executor.Exec(ctx, fmt.Sprintf("SET ROLE %v", runUID.String))
	return err
}

// ResetRole - RESET forms reset the current user identifier to be the current session user identifier
func (pge *PgEngine) ResetRole(ctx context.Context, executor executor) {
	l := log.GetLogger(ctx)
	l.Info("Resetting Role")
	_, err := executor.Exec(ctx, `RESET ROLE`)
	if err != nil {
		l.WithError(err).Error("Failed to set a role", err)
	}
}

// SetCurrentTaskContext - set the working transaction "pg_timetable.current_task_id" run-time parameter
func (pge *PgEngine) SetCurrentTaskContext(ctx context.Context, executor executor, chainID int, taskID int) {
	l := log.GetLogger(ctx)
	_, err := executor.Exec(ctx, `SELECT 
set_config('pg_timetable.current_task_id', $1, false),
set_config('pg_timetable.current_chain_id', $2, false),
set_config('pg_timetable.current_client_name', $3, false)`, strconv.Itoa(taskID), strconv.Itoa(chainID), pge.ClientName)
	if err != nil {
		l.WithError(err).Error("Failed to set current task context", err)
	}
}
