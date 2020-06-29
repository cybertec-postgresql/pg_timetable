package tasks

import (
	"context"

	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
)

func taskLog(ctx context.Context, val string) error {
	pgengine.LogToDB(ctx, "USER", val)
	return nil
}
