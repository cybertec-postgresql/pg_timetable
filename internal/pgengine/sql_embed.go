package pgengine

import (
	//use blank embed import
	_ "embed"
)

//go:embed sql/ddl.sql
var sqlDDL string

//go:embed sql/job_functions.sql
var sqlJobFunctions string

//go:embed sql/json_schema.sql
var sqlJSONSchema string

//go:embed sql/tasks.sql
var sqlTasks string
