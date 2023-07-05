package pgengine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/cybertec-postgresql/pg_timetable/internal/log"
	pgx "github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// StartTransaction returns transaction object, transaction id and error
func (pge *PgEngine) StartTransaction(ctx context.Context) (tx pgx.Tx, txid int64, err error) {
	if tx, err = pge.ConfigDb.Begin(ctx); err != nil {
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

// ExecuteSQLTask executes SQL task
func (pge *PgEngine) ExecuteSQLTask(ctx context.Context, tx pgx.Tx, task *ChainTask, paramValues []string) (out string, err error) {
	switch {
	case task.IsRemote():
		return pge.ExecRemoteSQLTask(ctx, task, paramValues)
	case task.Autonomous:
		return pge.ExecAutonomousSQLTask(ctx, task, paramValues)
	default:
		return pge.ExecLocalSQLTask(ctx, tx, task, paramValues)
	}
}

// ExecLocalSQLTask executes local task in the chain transaction
func (pge *PgEngine) ExecLocalSQLTask(ctx context.Context, tx pgx.Tx, task *ChainTask, paramValues []string) (out string, err error) {
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

// ExecStandaloneTask executes task against the provided connection interface, it can be remote connection or acquired connection from the pool
func (pge *PgEngine) ExecStandaloneTask(ctx context.Context, connf func() (PgxConnIface, error), task *ChainTask, paramValues []string) (string, error) {
	conn, err := connf()
	if err != nil {
		return "", err
	}
	defer pge.FinalizeDBConnection(ctx, conn)
	if err := pge.SetRole(ctx, conn, task.RunAs); err != nil {
		return "", err
	}
	pge.SetCurrentTaskContext(ctx, conn, task.ChainID, task.TaskID)
	return pge.ExecuteSQLCommand(ctx, conn, task.Script, paramValues)
}

// ExecRemoteSQLTask executes task against remote connection
func (pge *PgEngine) ExecRemoteSQLTask(ctx context.Context, task *ChainTask, paramValues []string) (string, error) {
	return pge.ExecStandaloneTask(ctx,
		func() (PgxConnIface, error) { return pge.GetRemoteDBConnection(ctx, task.ConnectString.String) },
		task, paramValues)
}

// ExecAutonomousSQLTask executes autonomous task in an acquired connection from pool
func (pge *PgEngine) ExecAutonomousSQLTask(ctx context.Context, task *ChainTask, paramValues []string) (string, error) {
	return pge.ExecStandaloneTask(ctx,
		func() (PgxConnIface, error) { return pge.GetLocalDBConnection(ctx) },
		task, paramValues)
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
		return
	}
	for _, val := range paramValues {
		if val > "" {
			if err = json.Unmarshal([]byte(val), &params); err != nil {
				return
			}
			ct, err = executor.Exec(ctx, command, params...)
			out = out + ct.String() + "\n"
		}
	}
	return
}

// GetLocalDBConnection acquires a connection from a local pool and returns it
func (pge *PgEngine) GetLocalDBConnection(ctx context.Context) (conn PgxConnIface, err error) {
	c, err := pge.ConfigDb.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	return c.Hijack(), nil
}

// GetRemoteDBConnection create a remote db connection object
func (pge *PgEngine) GetRemoteDBConnection(ctx context.Context, connectionString string) (conn PgxConnIface, err error) {
	conn, err = pgx.Connect(ctx, connectionString)
	if err != nil {
		return nil, err
	}
	log.GetLogger(ctx).Info("Remote connection established...")
	return
}

// FinalizeDBConnection closes session
func (pge *PgEngine) FinalizeDBConnection(ctx context.Context, remoteDb PgxConnIface) {
	l := log.GetLogger(ctx)
	l.Info("Closing remote session")
	if err := remoteDb.Close(ctx); err != nil {
		l.WithError(err).Error("Cannot close database connection:", err)
	}
	remoteDb = nil
}

// SetRole - set the current user identifier of the current session
func (pge *PgEngine) SetRole(ctx context.Context, executor executor, runUID pgtype.Text) error {
	if !runUID.Valid || strings.TrimSpace(runUID.String) == "" {
		return nil
	}
	log.GetLogger(ctx).Info("Setting Role to ", runUID.String)
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
	_, err := executor.Exec(ctx, `SELECT set_config('pg_timetable.current_task_id', $1, false),
set_config('pg_timetable.current_chain_id', $2, false),
set_config('pg_timetable.current_client_name', $3, false)`, strconv.Itoa(taskID), strconv.Itoa(chainID), pge.ClientName)
	if err != nil {
		log.GetLogger(ctx).WithError(err).Error("Failed to set current task context", err)
	}
}
