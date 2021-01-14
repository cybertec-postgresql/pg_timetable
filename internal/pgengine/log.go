package pgengine

import (
	"context"
	"fmt"
	"os"
	"time"
)

const (
	// black   = 30
	red    = 31
	green  = 32
	yellow = 33
	// purple  = 34
	magenta = 35
	blue    = 36
	//gray = 37
)

var levelColors = map[string]int{
	"PANIC":  red,
	"ERROR":  red,
	"REPAIR": red,
	"USER":   yellow,
	"LOG":    green,
	"NOTICE": magenta,
	"DEBUG":  blue}

// VerboseLogLevel specifies if log messages with level LOG should be logged
var VerboseLogLevel = true

func getColorizedPrefix(level string) string {
	return fmt.Sprintf("\x1b[%dm%s\x1b[0m", levelColors[level], time.Now().Format("2006-01-02 15:04:05.000")+" | "+level)
}

// GetLogPrefix perform formatted logging
func GetLogPrefix(level string) string {
	return fmt.Sprintf("[ %-40s ]: %%s", getColorizedPrefix(level))
}

const logTemplate = `INSERT INTO timetable.log(pid, client_name, log_level, message) VALUES ($1, $2, $3, $4)`

// LogToDB performs logging to standard output
func Log(level string, msg ...interface{}) {
	if !VerboseLogLevel {
		if level == "DEBUG" {
			return
		}
	}
	s := fmt.Sprintf(GetLogPrefix(level), fmt.Sprint(msg...))
	fmt.Println(s)
}

// LogToDB performs logging to configuration database ConfigDB initiated during bootstrap
func LogToDB(ctx context.Context, level string, msg ...interface{}) {
	if !VerboseLogLevel {
		if level == "DEBUG" {
			return
		}
	}
	Log(level, msg...)
	if ConfigDb != nil {
		_, err := ConfigDb.ExecContext(ctx, logTemplate, os.Getpid(), ClientName, level, fmt.Sprint(msg...))
		if err != nil {
			Log("ERROR", "Cannot log to the database: ", err)
		}
	}
}

// LogChainElementExecution will log current chain element execution status including retcode
func LogChainElementExecution(ctx context.Context, chainElemExec *ChainElementExecution, retCode int, output string) {
	_, err := ConfigDb.ExecContext(ctx, "INSERT INTO timetable.execution_log (chain_execution_config, chain_id, task_id, name, script, "+
		"kind, last_run, finished, returncode, pid, output, client_name) "+
		"VALUES ($1, $2, $3, $4, $5, $6, clock_timestamp() - $7 :: interval, clock_timestamp(), $8, $9, "+
		"NULLIF($10, ''), $11)",
		chainElemExec.ChainConfig, chainElemExec.ChainID, chainElemExec.TaskID, chainElemExec.TaskName,
		chainElemExec.Script, chainElemExec.Kind,
		fmt.Sprintf("%f seconds", float64(chainElemExec.Duration)/1000000),
		retCode, os.Getpid(), output, ClientName)
	if err != nil {
		LogToDB(ctx, "ERROR", "Error occurred during logging current chain element execution status including retcode: ", err)
	}
}
