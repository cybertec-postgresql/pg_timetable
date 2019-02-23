package tasks

import "github.com/cybertec-postgresql/pg_timetable/internal/pgengine"

func taskLog(val string) error {
	pgengine.LogToDB("USER", val)
	return nil
}
