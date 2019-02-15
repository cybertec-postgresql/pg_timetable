package pgengine

import (
	"github.com/jmoiron/sqlx"
)

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
