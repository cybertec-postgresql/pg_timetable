package pgengine

import (
	"context"
	"os"
)

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
