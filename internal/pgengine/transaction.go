package pgengine

import (
	"github.com/jmoiron/sqlx"
)

// ChainElementExecution structure describes each chain execution process
type ChainElementExecution struct {
	ChainConfig        int    `db:"chain_config"`
	ChainID            int    `db:"chain_id"`
	TaskID             int    `db:"task_id"`
	TaskName           string `db:"task_name"`
	Script             string `db:"script"`
	IsSQL              bool   `db:"is_sql"`
	RunUID             string `db:"run_uid"`
	IgnoreError        bool   `db:"ignore_error"`
	DatabaseConnection int    `db:"database_connection"`
	ConnectString      string `db:"connect_string"`
}

// StartTransaction return transaction object and panic in the case of error
func StartTransaction() *sqlx.Tx {
	return ConfigDb.MustBegin()
}

// MustCommitTransaction commits transaction and log panic in the case of error
func MustCommitTransaction(tx *sqlx.Tx) {
	err := tx.Commit()
	if err != nil {
		LogToDB("PANIC", "Application cannot commit after job finished: ", err)
	}
}

// GetChainElements returns all elements for a given chain
func GetChainElements(tx *sqlx.Tx, chains interface{}, chainID int) error {
	const sqlSelectChains = `
WITH RECURSIVE x
(chain_id, task_id, task_name, script, is_sql, run_uid, ignore_error, database_connection) AS 
(
	SELECT tc.chain_id, tc.task_id, bt.name, 
	bt.script, bt.is_sql, 
	tc.run_uid, 
	tc.ignore_error, 
	tc.database_connection 
	FROM timetable.task_chain tc JOIN 
	timetable.base_task bt USING (task_id) 
	WHERE tc.parent_id IS NULL AND tc.chain_id = $1 
	UNION ALL 
	SELECT tc.chain_id, tc.task_id, bt.name, 
	bt.script, bt.is_sql, 
	tc.run_uid, 
	tc.ignore_error, 
	tc.database_connection 
	FROM timetable.task_chain tc JOIN 
	timetable.base_task bt USING (task_id) JOIN 
	x ON (x.chain_id = tc.parent_id) 
) 
	SELECT *, (
		SELECT connect_string 
		FROM   timetable.database_connection AS a 
		WHERE a.database_connection = x.database_connection) 
	FROM x`

	err := tx.Select(chains, sqlSelectChains, chainID)

	if err != nil {
		LogToDB("ERROR", "Recursive queries to fetch task chain failed: ", err)
	}

	return err
}

// InsertChainRunStatus inits the execution run log, which will be use to effectively control scheduler concurrency
func InsertChainRunStatus(tx *sqlx.Tx, chainConfigID int, chainID int) int {
	const sqlInsertRunStatus = `INSERT INTO timetable.run_status 
(chain_id, execution_status, started, start_status, chain_execution_config) 
VALUES ($1, 'STARTED', now(), currval('timetable.run_status_run_status_seq'), $2) 
RETURNING run_status`
	var id int
	err := tx.Get(&id, sqlInsertRunStatus, chainID, chainConfigID)
	if err != nil {
		LogToDB("ERROR", "Cannot save information about the chain run status: ", err)
	}
	return id
}
