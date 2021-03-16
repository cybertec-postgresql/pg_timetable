package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/internal/tasks"
)

// Tasks maps builtin task names with event handlers
var Tasks = map[string](func(context.Context, *Scheduler, string) error){
	"NoOp":         taskNoOp,
	"Sleep":        taskSleep,
	"Log":          taskLog,
	"SendMail":     taskSendMail,
	"Download":     taskDownloadFile,
	"CopyFromFile": taskCopyFromFile}

func (sch *Scheduler) executeTask(ctx context.Context, name string, paramValues []string) error {
	f := Tasks[name]
	if f == nil {
		return errors.New("No built-in task found: " + name)
	}
	sch.pgengine.LogToDB(ctx, "DEBUG", fmt.Sprintf("Executing builtin task %s with parameters %v", name, paramValues))
	if len(paramValues) == 0 {
		return f(ctx, sch, "")
	}
	for _, val := range paramValues {
		if err := f(ctx, sch, val); err != nil {
			return err
		}
	}
	return nil
}

func taskNoOp(ctx context.Context, sch *Scheduler, val string) error {
	sch.pgengine.LogToDB(ctx, "DEBUG", "NoOp task called with value: ", val)
	return nil
}

func taskSleep(ctx context.Context, sch *Scheduler, val string) (err error) {
	var d int
	if d, err = strconv.Atoi(val); err != nil {
		return err
	}
	sch.pgengine.LogToDB(ctx, "DEBUG", "Sleep task called for ", d, " seconds")
	time.Sleep(time.Duration(d) * time.Second)
	return nil
}

func taskSendMail(ctx context.Context, sch *Scheduler, paramValues string) error {
	var conn tasks.EmailConn
	if err := json.Unmarshal([]byte(paramValues), &conn); err != nil {
		return err
	}
	if conn.ServerHost == "" {
		return errors.New("The IP address or hostname of the mail server not specified")
	}
	if conn.ServerPort == 0 {
		return errors.New("The port of the mail server not specified")
	}
	if conn.Username == "" {
		return errors.New("The username used for authenticating on the mail server not specified")
	}
	if conn.Password == "" {
		return errors.New("The password used for authenticating on the mail server not specified")
	}
	if conn.SenderAddr == "" {
		return errors.New("Sender address not specified")
	}
	if len(conn.ToAddr) == 0 && len(conn.CcAddr) == 0 && len(conn.BccAddr) == 0 {
		return errors.New("Recipient address not specified")
	}

	return tasks.SendMail(conn)
}

func taskLog(ctx context.Context, sch *Scheduler, val string) error {
	sch.pgengine.LogToDB(ctx, "USER", val)
	return nil
}

func taskCopyFromFile(ctx context.Context, sch *Scheduler, val string) error {
	type copyFrom struct {
		SQL      string `json:"sql"`
		Filename string `json:"filename"`
	}
	var ct copyFrom
	if err := json.Unmarshal([]byte(val), &ct); err != nil {
		return err
	}
	count, err := sch.pgengine.CopyFromFile(ctx, ct.Filename, ct.SQL)
	if err == nil {
		sch.pgengine.LogToDB(ctx, "LOG", fmt.Sprintf("%d rows copied from %s", count, ct.Filename))
	}
	return err
}

func taskDownloadFile(ctx context.Context, sch *Scheduler, paramValues string) error {
	type downloadOpts struct {
		WorkersNum int      `json:"workersnum"`
		FileUrls   []string `json:"fileurls"`
		DestPath   string   `json:"destpath"`
	}
	var opts downloadOpts
	if err := json.Unmarshal([]byte(paramValues), &opts); err != nil {
		return err
	}
	if len(opts.FileUrls) == 0 {
		return errors.New("Files to download are not specified")
	}
	if _, err := os.Stat(opts.DestPath); err != nil {
		return err
	}
	return tasks.DownloadUrls(ctx, opts.FileUrls, opts.DestPath, opts.WorkersNum)
}
