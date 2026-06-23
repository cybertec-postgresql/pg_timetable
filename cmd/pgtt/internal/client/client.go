// Package client is the internal data-access layer for the pgtt CLI.
//
// All database access goes through the Client interface so that the scriptable
// commands and a future TUI share exactly one implementation (PAT-003, GUD-002).
// The client connects directly to PostgreSQL and treats the timetable.* schema
// as the single source of truth (PAT-001). It MUST NOT create or upgrade the
// schema (REQ-016); that remains the responsibility of a pg_timetable instance.
package client

import (
	"context"

	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
)

// Chain is re-exported for command/rendering layers.
type Chain = pgengine.Chain

// ChainTask is re-exported for command/rendering layers.
type ChainTask = pgengine.ChainTask

// Session describes an active scheduler session (timetable.active_session).
type Session struct {
	ClientName string `db:"client_name" json:"client_name"`
	ClientPID  int64  `db:"client_pid" json:"client_pid"`
	ServerPID  int64  `db:"server_pid" json:"server_pid"`
	StartedAt  string `db:"started_at" json:"started_at"`
}

// ActiveChain describes a currently running chain (timetable.active_chain).
type ActiveChain struct {
	ChainID    int    `db:"chain_id" json:"chain_id"`
	ClientName string `db:"client_name" json:"client_name"`
	StartedAt  string `db:"started_at" json:"started_at"`
}

// ChainListItem is a chain row enriched with derived status for `chain list`.
type ChainListItem struct {
	Chain
	ClientName string `db:"client_name" json:"client_name"`
	RunAt      string `db:"run_at" json:"run_at"`
	Live       bool   `db:"live" json:"live"`
	Active     bool   `db:"-" json:"active"`
	LastStatus string `db:"-" json:"last_status"`
}

// LogEntry is a single log line (timetable.log).
type LogEntry struct {
	TS         string `db:"ts" json:"ts"`
	PID        int    `db:"pid" json:"pid"`
	LogLevel   string `db:"log_level" json:"log_level"`
	ClientName string `db:"client_name" json:"client_name"`
	Message    string `db:"message" json:"message"`
}

// LogFilter narrows log queries.
type LogFilter struct {
	ChainID    int    // 0 means "any chain"
	ClientName string // "" means "any client"
	Limit      int    // 0 means use a sane default
}

// Client is the full management surface used by pgtt commands and (later) the TUI.
//
// Phase 1 implements Connect/Close and CheckSchemaVersion. The remaining methods
// define the deliberate interface shape for Phases 2-5; their implementations are
// completed in those phases.
type Client interface {
	// Connect opens a connection pool to PostgreSQL. It does NOT create the schema.
	Connect(ctx context.Context, dsn string) error
	// Close releases the connection pool.
	Close()
	// CheckSchemaVersion verifies the timetable.* schema is present and compatible
	// with the version pgtt was built for (REQ-016 / AC-009).
	CheckSchemaVersion(ctx context.Context) error

	// --- Read & observe (Phase 2) ---
	ListChains(ctx context.Context) ([]ChainListItem, error)
	ShowChain(ctx context.Context, ref string) (*ChainListItem, []ChainTask, error)
	ListSessions(ctx context.Context) ([]Session, error)
	ListActiveChains(ctx context.Context) ([]ActiveChain, error)
	ListLogs(ctx context.Context, f LogFilter) ([]LogEntry, error)

	// --- Live control (Phase 3) ---
	StartChain(ctx context.Context, chainID int, worker string, delaySeconds int) error
	StopChain(ctx context.Context, chainID int, worker string) error
	PauseChain(ctx context.Context, ref string) error
	ResumeChain(ctx context.Context, ref string) error
}
