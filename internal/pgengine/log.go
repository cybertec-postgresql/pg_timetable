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
	// magenta = 35
	blue = 36
	gray = 37
)

var levelColors = map[string]int{
	"PANIC":  red,
	"ERROR":  red,
	"USER":   yellow,
	"LOG":    blue,
	"NOTICE": green,
	"DEBUG":  gray}

// VerboseLogLevel specifies if log messages with level LOG should be logged
var VerboseLogLevel = true

func getColorizedLevel(level string) string {
	return fmt.Sprintf("\x1b[%dm%s\x1b[0m", levelColors[level], level)
}

// GetLogPrefix perform formatted logging
func GetLogPrefix(level string) string {
	return fmt.Sprintf("[%v | %s | %-15s]: %%s", time.Now().Format("2006-01-02 15:04:05.000"), ClientName, getColorizedLevel(level))
}

// GetLogPrefixLn perform formatted logging with new line at the end
func GetLogPrefixLn(level string) string {
	return GetLogPrefix(level) + "\n"
}

// LogToDB performs logging to configuration database ConfigDB initiated during bootstrap
func LogToDB(level string, msg ...interface{}) {
	const logTemplate = `INSERT INTO timetable.log(pid, client_name, log_level, message) VALUES ($1, $2, $3, $4)`
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
		for err != nil && ConfigDb.Ping() != nil {
			// If there is DB outage, reconnect and write missing log
			ReconnectDbAndFixLeftovers()
			_, err = ConfigDb.Exec(logTemplate, os.Getpid(), ClientName, level, fmt.Sprint(msg...))
			level = "ERROR" //we don't want panic in case of disconnect
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
