package pgengine

import (
	//use blank embed import
	_ "embed"
)

//go:embed sql/ddl.sql
var sqlDDL string

//go:embed sql/cron_functions.sql
var sqlCronFunctions string

//go:embed sql/job_functions.sql
var sqlJobFunctions string

//go:embed sql/json_schema.sql
var sqlJSONSchema string
