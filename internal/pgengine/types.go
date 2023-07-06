package pgengine

import (
	"context"
	"strings"
	"time"

	pgconn "github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type executor interface {
	Exec(ctx context.Context, sql string, arguments ...interface{}) (commandTag pgconn.CommandTag, err error)
}

// Chain structure used to represent tasks chains
type Chain struct {
	ChainID            int         `db:"chain_id"`
	ChainName          string      `db:"chain_name"`
	SelfDestruct       bool        `db:"self_destruct"`
	ExclusiveExecution bool        `db:"exclusive_execution"`
	MaxInstances       int         `db:"max_instances"`
	Timeout            int         `db:"timeout"`
	OnErrorSQL         pgtype.Text `db:"on_error"`
}

// IntervalChain structure used to represent repeated chains.
type IntervalChain struct {
	Chain
	Interval    int  `db:"interval_seconds"`
	RepeatAfter bool `db:"repeat_after"`
}

func (ichain IntervalChain) IsListed(ichains []IntervalChain) bool {
	for _, ic := range ichains {
		if ichain.ChainID == ic.ChainID {
			return true
		}
	}
	return false
}

// ChainTask structure describes each chain task
type ChainTask struct {
	ChainID       int         `db:"-"`
	TaskID        int         `db:"task_id"`
	Script        string      `db:"command"`
	Kind          string      `db:"kind"`
	RunAs         pgtype.Text `db:"run_as"`
	IgnoreError   bool        `db:"ignore_error"`
	Autonomous    bool        `db:"autonomous"`
	ConnectString pgtype.Text `db:"database_connection"`
	Timeout       int         `db:"timeout"` // in milliseconds
	StartedAt     time.Time   `db:"-"`
	Duration      int64       `db:"-"` // in microseconds
	Txid          int64       `db:"-"`
}

func (task *ChainTask) IsRemote() bool {
	return task.ConnectString.Valid && strings.TrimSpace(task.ConnectString.String) != ""
}
