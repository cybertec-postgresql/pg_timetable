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
)

// StartTransaction returns transaction object, virtual transaction id and error
func (pge *PgEngine) StartTransaction(ctx context.Context) (tx pgx.Tx, vxid int64, err error) {
	if tx, err = pge.ConfigDb.Begin(ctx); err != nil {
		return
	}
	err = tx.QueryRow(ctx, `SELECT 
(split_part(virtualxid, '/', 1)::int8 << 32) | split_part(virtualxid, '/', 2)::int8
FROM pg_locks 
WHERE pid = pg_backend_pid() AND virtualxid IS NOT NULL`).Scan(&vxid)
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
func (pge *PgEngine) ExecuteSQLTask(ctx context.Context, tx pgx.Tx, task *ChainTask, paramValues []string) (err error) {
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
func (pge *PgEngine) ExecLocalSQLTask(ctx context.Context, tx pgx.Tx, task *ChainTask, paramValues []string) (err error) {
	if err := pge.SetRole(ctx, tx, task.RunAs); err != nil {
		return err
	}
	if task.IgnoreError {
		pge.MustSavepoint(ctx, tx, task.TaskID)
	}
	pge.SetCurrentTaskContext(ctx, tx, task.ChainID, task.TaskID)
	err = pge.ExecuteSQLCommand(ctx, tx, task, paramValues)
	if err != nil && task.IgnoreError {
		pge.MustRollbackToSavepoint(ctx, tx, task.TaskID)
	}
	if task.RunAs > "" {
		pge.ResetRole(ctx, tx)
	}
	return
}

// ExecStandaloneTask executes task against the provided connection interface, it can be remote connection or acquired connection from the pool
func (pge *PgEngine) ExecStandaloneTask(ctx context.Context, connf func() (PgxConnIface, error), task *ChainTask, paramValues []string) error {
	conn, err := connf()
	if err != nil {
		return err
	}
	defer pge.FinalizeDBConnection(ctx, conn)
	if err := pge.SetRole(ctx, conn, task.RunAs); err != nil {
		return err
	}
	pge.SetCurrentTaskContext(ctx, conn, task.ChainID, task.TaskID)
	return pge.ExecuteSQLCommand(ctx, conn, task, paramValues)
}

// ExecRemoteSQLTask executes task against remote connection
func (pge *PgEngine) ExecRemoteSQLTask(ctx context.Context, task *ChainTask, paramValues []string) error {
	log.GetLogger(ctx).Info("Switching to remote task mode")
	return pge.ExecStandaloneTask(ctx,
		func() (PgxConnIface, error) { return pge.GetRemoteDBConnection(ctx, task.ConnectString) },
		task, paramValues)
}

// ExecAutonomousSQLTask executes autonomous task in an acquired connection from pool
func (pge *PgEngine) ExecAutonomousSQLTask(ctx context.Context, task *ChainTask, paramValues []string) error {
	log.GetLogger(ctx).Info("Switching to autonomous task mode")
	return pge.ExecStandaloneTask(ctx,
		func() (PgxConnIface, error) { return pge.GetLocalDBConnection(ctx) },
		task, paramValues)
}

// ExecuteSQLCommand executes chain command with parameters inside transaction
func (pge *PgEngine) ExecuteSQLCommand(ctx context.Context, executor executor, task *ChainTask, paramValues []string) (err error) {
	var params []any
	var errCodes = map[bool]int{false: 0, true: -1}
	if strings.TrimSpace(task.Command) == "" {
		return errors.New("SQL command cannot be empty")
	}
	if len(paramValues) == 0 { //mimic empty param
		ct, e := executor.Exec(ctx, task.Command)
		pge.LogTaskExecution(context.Background(), task, errCodes[err != nil], ct.String(), "")
		return e
	}
	for _, val := range paramValues {
		if val == "" {
			continue
		}
		if err = json.Unmarshal([]byte(val), &params); err != nil {
			return
		}
		ct, e := executor.Exec(ctx, task.Command, params...)
		err = errors.Join(err, e)
		pge.LogTaskExecution(context.Background(), task, errCodes[e != nil], ct.String(), val)
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
	l.Info("Closing standalone session")
	if err := remoteDb.Close(ctx); err != nil {
		l.WithError(err).Error("Cannot close database connection:", err)
	}
	remoteDb = nil
}

// SetRole - set the current user identifier of the current session
func (pge *PgEngine) SetRole(ctx context.Context, executor executor, runUID string) error {
	if strings.TrimSpace(runUID) == "" {
		return nil
	}
	log.GetLogger(ctx).Info("Setting role to ", runUID)
	_, err := executor.Exec(ctx, fmt.Sprintf("SET ROLE %v", runUID))
	return err
}

// ResetRole - RESET forms reset the current user identifier to be the current session user identifier
func (pge *PgEngine) ResetRole(ctx context.Context, executor executor) {
	l := log.GetLogger(ctx)
	l.Info("Resetting role")
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
