package pgengine

import (
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

// GetLogPrefixLn perform formatted logging with new line at the end
func GetLogPrefixLn(level string) string {
	return GetLogPrefix(level) + "\n"
}

const logTemplate = `INSERT INTO timetable.log(pid, client_name, log_level, message) VALUES ($1, $2, $3, $4)`

// LogToDB performs logging to configuration database ConfigDB initiated during bootstrap
func LogToDB(level string, msg ...interface{}) {
	if !VerboseLogLevel {
		switch level {
		case
			"DEBUG", "NOTICE":
			return
		}
	}
	s := fmt.Sprintf(GetLogPrefix(level), fmt.Sprint(msg...))
	fmt.Println(s)
	if ConfigDb != nil {
		_, err := ConfigDb.Exec(logTemplate, os.Getpid(), ClientName, level, fmt.Sprint(msg...))
		if err != nil {
			fmt.Printf(GetLogPrefixLn("ERROR"), fmt.Sprint("Cannot log to the database: ", err))
		}
	}
}

// LogChainElementExecution will log current chain element execution status including retcode
func LogChainElementExecution(chainElemExec *ChainElementExecution, retCode int, output string) {
	_, err := ConfigDb.Exec("INSERT INTO timetable.execution_log (chain_execution_config, chain_id, task_id, name, script, "+
		"kind, last_run, finished, returncode, pid, output, client_name) "+
		"VALUES ($1, $2, $3, $4, $5, $6, clock_timestamp() - $7 :: interval, clock_timestamp(), $8, $9, "+
		"NULLIF($10, ''), $11)",
		chainElemExec.ChainConfig, chainElemExec.ChainID, chainElemExec.TaskID, chainElemExec.TaskName,
		chainElemExec.Script, chainElemExec.Kind,
		fmt.Sprintf("%d microsecond", chainElemExec.Duration),
		retCode, os.Getpid(), output, ClientName)
	if err != nil {
		LogToDB("ERROR", "Error occurred during logging current chain element execution status including retcode: ", err)
	}
}
