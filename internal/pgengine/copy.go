package pgengine

import (
	"context"
	"os"
)

func CopyFromFile(ctx context.Context, filename string, sql string) (int64, error) {
	dbconn, err := ConfigDb.Acquire(ctx)
	if err != nil {
		return -1, err
	}
	defer dbconn.Release()
	f, err := os.Open(filename)
	if err != nil {
		return -1, err
	}
	res, err := dbconn.Conn().PgConn().CopyFrom(ctx, f, sql)
	return res.RowsAffected(), err
}
