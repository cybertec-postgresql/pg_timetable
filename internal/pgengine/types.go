package pgengine

import (
	"context"
	"strings"
	"time"

	pgconn "github.com/jackc/pgx/v5/pgconn"
)

type executor interface {
	Exec(ctx context.Context, sql string, arguments ...interface{}) (commandTag pgconn.CommandTag, err error)
}

// Chain structure used to represent tasks chains
type Chain struct {
	ChainID            int    `db:"chain_id" yaml:"-"`
	ChainName          string `db:"chain_name" yaml:"name"`
	SelfDestruct       bool   `db:"self_destruct" yaml:"self_destruct,omitempty"`
	ExclusiveExecution bool   `db:"exclusive_execution" yaml:"exclusive,omitempty"`
	MaxInstances       int    `db:"max_instances" yaml:"max_instances,omitempty"`
	Timeout            int    `db:"timeout" yaml:"timeout,omitempty"`
	OnError            string `db:"on_error" yaml:"on_error,omitempty"`
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
	ChainID       int       `db:"-" yaml:"-"`
	TaskID        int       `db:"task_id" yaml:"-"`
	Command       string    `db:"command" yaml:"command"`
	Kind          string    `db:"kind" yaml:"kind,omitempty"`
	RunAs         string    `db:"run_as" yaml:"run_as,omitempty"`
	IgnoreError   bool      `db:"ignore_error" yaml:"ignore_error,omitempty"`
	Autonomous    bool      `db:"autonomous" yaml:"autonomous,omitempty"`
	ConnectString string    `db:"database_connection" yaml:"connect_string,omitempty"`
	Timeout       int       `db:"timeout" yaml:"timeout,omitempty"` // in milliseconds
	StartedAt     time.Time `db:"-" yaml:"-"`
	Duration      int64     `db:"-" yaml:"-"` // in microseconds
	Vxid          int64     `db:"-" yaml:"-"`
}

func (task *ChainTask) IsRemote() bool {
	return strings.TrimSpace(task.ConnectString) != ""
}
