package pgengine

import (
	"context"
	"errors"
	"os"
	"os/exec"
)

// CopyToFile copies data from database into local file using COPY format specified by sql
func (pge *PgEngine) CopyToFile(ctx context.Context, filename string, sql string) (int64, error) {
	dbconn, err := pge.ConfigDb.Acquire(ctx)
	if err != nil {
		return -1, err
	}
	defer dbconn.Release()
	f, err := os.Create(filename)
	defer func() { _ = f.Close() }()
	if err != nil {
		return -1, err
	}
	res, err := dbconn.Conn().PgConn().CopyTo(ctx, f, sql)
	return res.RowsAffected(), err
}

// CopyFromFile copies data from local file into database using COPY format specified by sql
func (pge *PgEngine) CopyFromFile(ctx context.Context, filename string, sql string) (int64, error) {
	dbconn, err := pge.ConfigDb.Acquire(ctx)
	if err != nil {
		return -1, err
	}
	defer dbconn.Release()
	f, err := os.Open(filename)
	defer func() { _ = f.Close() }()
	if err != nil {
		return -1, err
	}
	res, err := dbconn.Conn().PgConn().CopyFrom(ctx, f, sql)
	return res.RowsAffected(), err
}

// CopyToProgram copies data from database to the standard input of the command using COPY format specified by sql
func (pge *PgEngine) CopyToProgram(ctx context.Context, sql string, cmd string, args ...string) (int64, error) {
	dbconn, err := pge.ConfigDb.Acquire(ctx)
	if err != nil {
		return -1, err
	}
	defer dbconn.Release()
	c := exec.CommandContext(ctx, cmd, args...)
	inPipe, err := c.StdinPipe()
	if err != nil {
		return -1, err
	}
	if err := c.Start(); err != nil {
		return -1, err
	}
	res, sqlErr := dbconn.Conn().PgConn().CopyTo(ctx, inPipe, sql)
	_ = inPipe.Close()
	cmdError := c.Wait()
	return res.RowsAffected(), errors.Join(sqlErr, cmdError)
}

// CopyFromProgram copies data from the standard output of the command into database using COPY format specified by sql
func (pge *PgEngine) CopyFromProgram(ctx context.Context, sql string, cmd string, args ...string) (int64, error) {
	dbconn, err := pge.ConfigDb.Acquire(ctx)
	if err != nil {
		return -1, err
	}
	defer dbconn.Release()
	c := exec.CommandContext(ctx, cmd, args...)
	outPipe, err := c.StdoutPipe()
	if err != nil {
		return -1, err
	}
	if err := c.Start(); err != nil {
		return -1, err
	}
	res, err := dbconn.Conn().PgConn().CopyFrom(ctx, outPipe, sql)
	waitErr := c.Wait()
	return res.RowsAffected(), errors.Join(waitErr, err)
}
