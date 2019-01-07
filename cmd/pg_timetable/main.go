package main

import "fmt"

func main() {
	usage()
}

/* display the syntax of this program */
func usage() {
	fmt.Print(`
pg_timetable - task scheduler and executor
Usage: pg_timetable [options] 
  -f: configuration file used to configure the connect string.
  -d  defines the number of days we want to keep in the run log
  -h  display usage
Reports bugs to https://github.com/cybertec-postgresql/pg_timetable`)
	return
}
