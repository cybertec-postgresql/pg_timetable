package tasks

import (
	"strconv"
	"time"
)

// Tasks maps builtin task names with event handlers
var Tasks = map[string](func(string) error){
	"NoOp":     taskNoOp,
	"Sleep":    taskSleep,
	"Log":      taskLog,
	"SendMail": taskSendMail,
	"Download": taskDownloadFile}

// ExecuteTask executes built-in task depending on task name and returns err result
func ExecuteTask(name string, paramValues []string) error {
	for _, val := range paramValues {
		err := Tasks[name](val)
		if err != nil {
			return err
		}
	}
	return nil
}

func taskNoOp(val string) error {
	return nil
}

func taskSleep(val string) (err error) {
	var d int
	if d, err = strconv.Atoi(val); err != nil {
		return err
	}
	time.Sleep(time.Duration(d) * time.Second)
	return nil
}
