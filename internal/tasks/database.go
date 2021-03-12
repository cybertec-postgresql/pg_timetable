package tasks

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
)

func taskLog(ctx context.Context, val string) error {
	pgengine.LogToDB(ctx, "USER", val)
	return nil
}

type copyFrom struct {
	SQL      string `json:"sql"`
	Filename string `json:"filename"`
}

func taskCopyFromFile(ctx context.Context, val string) error {
	var ct copyFrom
	if err := json.Unmarshal([]byte(val), &ct); err != nil {
		return err
	}
	count, err := pgengine.CopyFromFile(ctx, ct.Filename, ct.SQL)
	if err == nil {
		pgengine.LogToDB(ctx, "DEBUG", fmt.Sprintf("%d rows copied from %s", count, ct.Filename))
	}
	return err
}
