package tasks

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
)

// Tasks maps builtin task names with event handlers
var Tasks = map[string](func(context.Context, string) error){
	"NoOp":         taskNoOp,
	"Sleep":        taskSleep,
	"Log":          taskLog,
	"SendMail":     taskSendMail,
	"Download":     taskDownloadFile,
	"CopyFromFile": taskCopyFromFile}

// ExecuteTask executes built-in task depending on task name and returns err result
func ExecuteTask(ctx context.Context, name string, paramValues []string) error {
	f := Tasks[name]
	if f == nil {
		return errors.New("No built-in task found: " + name)
	}
	pgengine.LogToDB(ctx, "DEBUG", fmt.Sprintf("Executing builtin task %s with parameters %v", name, paramValues))
	if len(paramValues) == 0 {
		return f(ctx, "")
	}
	for _, val := range paramValues {
		if err := f(ctx, val); err != nil {
			return err
		}
	}
	return nil
}

func taskNoOp(ctx context.Context, val string) error {
	pgengine.LogToDB(ctx, "DEBUG", "NoOp task called with value: ", val)
	return nil
}

func taskSleep(ctx context.Context, val string) (err error) {
	var d int
	if d, err = strconv.Atoi(val); err != nil {
		return err
	}
	pgengine.LogToDB(ctx, "DEBUG", "Sleep task called for ", d, " seconds")
	time.Sleep(time.Duration(d) * time.Second)
	return nil
}
