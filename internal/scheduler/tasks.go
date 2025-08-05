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

// BuiltinTasks maps builtin task names with event handlers
var BuiltinTasks = map[string](func(context.Context, *Scheduler, string) (string, error)){
	"NoOp":            taskNoOp,
	"Sleep":           taskSleep,
	"Log":             taskLog,
	"SendMail":        taskSendMail,
	"Download":        taskDownload,
	"CopyFromFile":    taskCopyFromFile,
	"CopyToFile":      taskCopyToFile,
	"CopyToProgram":   taskCopyToProgram,
	"CopyFromProgram": taskCopyFromProgram,
	"Shutdown":        taskShutdown}

func (sch *Scheduler) executeBuiltinTask(ctx context.Context, name string, paramValues []string) (stdout string, err error) {
	var s string
	f := BuiltinTasks[name]
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
		stdout += fmt.Sprintln(s)
	}
	return
}

func taskNoOp(_ context.Context, _ *Scheduler, val string) (stdout string, err error) {
	return "NoOp task called with value: " + val, nil
}

func taskSleep(ctx context.Context, _ *Scheduler, val string) (stdout string, err error) {
	var d int
	if d, err = strconv.Atoi(val); err != nil {
		return "", err
	}
	dur := time.Duration(d) * time.Second
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(dur):
		return "Sleep task called for " + dur.String(), nil
	}
}

func taskLog(ctx context.Context, _ *Scheduler, val string) (stdout string, err error) {
	log.GetLogger(ctx).Print(val)
	return "Logged: " + val, nil
}

func taskSendMail(ctx context.Context, _ *Scheduler, paramValues string) (stdout string, err error) {
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

func taskCopyToProgram(ctx context.Context, sch *Scheduler, val string) (stdout string, err error) {
	type copyToProgram struct {
		SQL  string   `json:"sql"`
		Cmd  string   `json:"cmd"`
		Args []string `json:"args"`
	}
	var ctp copyToProgram
	if err := json.Unmarshal([]byte(val), &ctp); err != nil {
		return "", err
	}
	count, err := sch.pgengine.CopyToProgram(ctx, ctp.SQL, ctp.Cmd, ctp.Args...)
	if err == nil {
		stdout = fmt.Sprintf("%d rows copied to program %s", count, ctp.Cmd)
	}
	return stdout, err
}

func taskCopyFromProgram(ctx context.Context, sch *Scheduler, val string) (stdout string, err error) {
	type copyFromProgram struct {
		SQL  string   `json:"sql"`
		Cmd  string   `json:"cmd"`
		Args []string `json:"args"`
	}
	var cfp copyFromProgram
	if err := json.Unmarshal([]byte(val), &cfp); err != nil {
		return "", err
	}
	count, err := sch.pgengine.CopyFromProgram(ctx, cfp.SQL, cfp.Cmd, cfp.Args...)
	if err == nil {
		stdout = fmt.Sprintf("%d rows copied from program %s", count, cfp.Cmd)
	}
	return stdout, err
}

func taskDownload(ctx context.Context, _ *Scheduler, paramValues string) (stdout string, err error) {
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
		return "", errors.New("files to download are not specified")
	}
	return tasks.DownloadUrls(ctx, opts.FileUrls, opts.DestPath, opts.WorkersNum)
}

func taskShutdown(_ context.Context, sch *Scheduler, val string) (stdout string, err error) {
	sch.l.WithField("message", val).Info("Shutdown command received")
	sch.Shutdown()
	return "Shutdown task called", nil
}
