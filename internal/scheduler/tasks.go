package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/internal/log"
	"github.com/cybertec-postgresql/pg_timetable/internal/tasks"
)

// Tasks maps builtin task names with event handlers
var Tasks = map[string](func(context.Context, *Scheduler, string) (string, error)){
	"NoOp":         taskNoOp,
	"Sleep":        taskSleep,
	"Log":          taskLog,
	"SendMail":     taskSendMail,
	"Download":     taskDownload,
	"CopyFromFile": taskCopyFromFile,
	"CopyToFile":   taskCopyToFile,
	"Shutdown":     taskShutdown}

func (sch *Scheduler) executeTask(ctx context.Context, name string, paramValues []string) (stdout string, err error) {
	var s string
	f := Tasks[name]
	if f == nil {
		return "", errors.New("No built-in task found: " + name)
	}
	l := log.GetLogger(ctx)
	l.WithField("name", name).Debugf("Executing builtin task with parameters %+q", paramValues)
	if len(paramValues) == 0 {
		return f(ctx, sch, "")
	}
	for _, val := range paramValues {
		if s, err = f(ctx, sch, val); err != nil {
			return
		}
		stdout = stdout + fmt.Sprintln(s)
	}
	return
}

func taskNoOp(ctx context.Context, sch *Scheduler, val string) (stdout string, err error) {
	return "NoOp task called with value: " + val, nil
}

func taskSleep(ctx context.Context, sch *Scheduler, val string) (stdout string, err error) {
	var d int
	if d, err = strconv.Atoi(val); err != nil {
		return "", err
	}
	dur := time.Duration(d) * time.Second
	time.Sleep(dur)
	return "Sleep task called for " + dur.String(), nil
}

func taskLog(ctx context.Context, sch *Scheduler, val string) (stdout string, err error) {
	log.GetLogger(ctx).Print(val)
	return "Logged: " + val, nil
}

func taskSendMail(ctx context.Context, sch *Scheduler, paramValues string) (stdout string, err error) {
	conn := tasks.EmailConn{ServerPort: 587, ContentType: "text/plain"}
	if err := json.Unmarshal([]byte(paramValues), &conn); err != nil {
		return "", err
	}
	return "", tasks.SendMail(ctx, conn)
}

func taskCopyFromFile(ctx context.Context, sch *Scheduler, val string) (stdout string, err error) {
	type copyFrom struct {
		SQL      string `json:"sql"`
		Filename string `json:"filename"`
	}
	var ct copyFrom
	if err := json.Unmarshal([]byte(val), &ct); err != nil {
		return "", err
	}
	count, err := sch.pgengine.CopyFromFile(ctx, ct.Filename, ct.SQL)
	if err == nil {
		stdout = fmt.Sprintf("%d rows copied from %s", count, ct.Filename)
	}
	return stdout, err
}

func taskCopyToFile(ctx context.Context, sch *Scheduler, val string) (stdout string, err error) {
	type copyTo struct {
		SQL      string `json:"sql"`
		Filename string `json:"filename"`
	}
	var ct copyTo
	if err := json.Unmarshal([]byte(val), &ct); err != nil {
		return "", err
	}
	count, err := sch.pgengine.CopyToFile(ctx, ct.Filename, ct.SQL)
	if err == nil {
		stdout = fmt.Sprintf("%d rows copied to %s", count, ct.Filename)
	}
	return stdout, err
}

func taskDownload(ctx context.Context, sch *Scheduler, paramValues string) (stdout string, err error) {
	type downloadOpts struct {
		WorkersNum int      `json:"workersnum"`
		FileUrls   []string `json:"fileurls"`
		DestPath   string   `json:"destpath"`
	}
	var opts downloadOpts
	if err := json.Unmarshal([]byte(paramValues), &opts); err != nil {
		return "", err
	}
	if len(opts.FileUrls) == 0 {
		return "", errors.New("Files to download are not specified")
	}
	return tasks.DownloadUrls(ctx, opts.FileUrls, opts.DestPath, opts.WorkersNum)
}

func taskShutdown(ctx context.Context, sch *Scheduler, val string) (stdout string, err error) {
	sch.l.Debug("Shutdown command received...")
	sch.Shutdown()
	return "Shutdown task called", nil
}
