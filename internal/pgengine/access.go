package pgengine

import (
	"fmt"
	"os"
)

//VerboseLogLevel specifies if log messages with level LOG should be logged
var VerboseLogLevel = true

// LogToDB performs logging to configuration database ConfigDB initiated during bootstrap
func LogToDB(instanceID int, level string, msg ...interface{}) {
	if level == "LOG" && !VerboseLogLevel {
		return
	}
	fmt.Printf("[%s:%d]:\t%s\n", level, instanceID, fmt.Sprint(msg...))
	if instanceID == 0 {
		ConfigDb.MustExec(`INSERT INTO pg_timetable.t_log(pid, database_host_id, log_level, message) 
				VALUES ($1, NULL, $2, $3)`, os.Getpid(), level, fmt.Sprint(msg...))
	} else {
		ConfigDb.MustExec(`INSERT INTO pg_timetable.t_log(pid, database_host_id, log_level, message) 
			VALUES ($1, $2, $3, $4)`, os.Getpid(), instanceID, level, fmt.Sprint(msg...))
	}
	if level == "PANIC" {
		panic(fmt.Sprint(msg...))
	}
}

/*FixSchedulerCrash make sure that task chains which are not complete due to a scheduler crash are "fixed"
and marked as stopped at a certain point */
func FixSchedulerCrash() {
	ConfigDb.MustExec(
		`INSERT INTO pg_timetable.run_status (execution_status, started, last_status_update, start_status)
  SELECT 'SCHEDULER_DEATH', now(), now(), start_status FROM (
   SELECT   start_status
     FROM   pg_timetable.run_status
     WHERE   execution_status IN ('STARTED', 'CHAIN_FAILED',
                'CHAIN_DONE', 'SCHEDULER_DEATH')
     GROUP BY 1
     HAVING count(*) < 2 ) AS abc`)
}
