package pgengine

import (
	"fmt"
	"os"
)

//VerboseLogLevel specifies if log messages with level LOG should be logged
var VerboseLogLevel = true

// LogToDB performs logging to configuration database ConfigDB initiated during bootstrap
func LogToDB(clientID string, level string, msg ...interface{}) {
	if level == "LOG" && !VerboseLogLevel {
		return
	}
	ConfigDb.MustExec(`INSERT INTO timetable.log(pid, client_name, log_level, message) 
		VALUES ($1, $2, $3, $4)`, os.Getpid(), clientID, level, fmt.Sprint(msg...))
	s := fmt.Sprintf("[%s:%s]:\t%s\n", level, clientID, fmt.Sprint(msg...))
	if level == "PANIC" {
		panic(s)
	} else {
		fmt.Println(s)
	}
}

/*FixSchedulerCrash make sure that task chains which are not complete due to a scheduler crash are "fixed"
and marked as stopped at a certain point */
func FixSchedulerCrash() {
	ConfigDb.MustExec(
		`INSERT INTO timetable.run_status (execution_status, started, last_status_update, start_status)
  SELECT 'SCHEDULER_DEATH', now(), now(), start_status FROM (
   SELECT   start_status
     FROM   timetable.run_status
     WHERE   execution_status IN ('STARTED', 'CHAIN_FAILED',
                'CHAIN_DONE', 'SCHEDULER_DEATH')
     GROUP BY 1
     HAVING count(*) < 2 ) AS abc`)
}
