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
	ChainName  string `db:"chain_name" json:"chain_name"`
	ClientName string `db:"client_name" json:"client_name"`
	StartedAt  string `db:"started_at" json:"started_at"`
}

// ChainListItem is a chain row enriched with derived status for `chain list`.
type ChainListItem struct {
	Chain
	ClientName     string `db:"client_name" json:"client_name"`
	RunAt          string `db:"run_at" json:"run_at"`
	Live           bool   `db:"live" json:"live"`
	Active         bool   `db:"active" json:"active"`
	LastStatus     string `db:"last_status" json:"last_status"`
	LastRun        string `db:"last_run" json:"last_run"`
	LastDurationMS int64  `db:"last_duration_ms" json:"last_duration_ms"`
	LastReturncode int    `db:"last_returncode" json:"last_returncode"`
	LastWorker     string `db:"last_worker" json:"last_worker"`
}

// LogEntry is a single log line (timetable.log).
type LogEntry struct {
	TS         string `db:"ts" json:"ts"`
	PID        int    `db:"pid" json:"pid"`
	LogLevel   string `db:"log_level" json:"log_level"`
	ClientName string `db:"client_name" json:"client_name"`
	Message    string `db:"message" json:"message"`
}

// ActivityEntry is a unified row merging timetable.log and timetable.execution_log
// into a single chronological activity stream.
//
// Source == "log"      → scheduler diagnostic message (timetable.log)
// Source == "exec"     → task execution result (timetable.execution_log)
type ActivityEntry struct {
	TS         string `db:"ts" json:"ts"`
	Source     string `db:"source" json:"source"`
	ClientName string `db:"client_name" json:"client_name"`
	ChainID    int64  `db:"chain_id" json:"chain_id"`
	ChainName  string `db:"chain_name" json:"chain_name"`
	TaskID     int64  `db:"task_id" json:"task_id"`
	// Vxid is the virtual transaction id (e.g. "21474836598"). It is kept as a
	// string because virtual xids combine a backend id with a local counter and
	// can exceed the range/meaning of a plain integer.
	Vxid       string `db:"vxid" json:"vxid"`
	Level      string `db:"level" json:"level"` // log level / PG severity / "OK"/"FAIL"
	Returncode int    `db:"returncode" json:"returncode"`
	DurationMS int64  `db:"duration_ms" json:"duration_ms"`
	Message    string `db:"message" json:"message"` // log message or task output
	Command    string `db:"command" json:"command"`
	// Notice/Severity carry PostgreSQL NOTICE/WARNING context captured by the
	// scheduler's OnNotice handler (message_data->>'notice' / ->>'severity').
	// They are empty for rows that are not server notices.
	Notice   string `db:"notice" json:"notice"`
	Severity string `db:"severity" json:"severity"`
	// IsHeader is set only by ListActivityTree: it marks the first line of a
	// chain run (the branch header) so the renderer needs no grouping logic.
	// Always false for the flat ListActivity feed.
	IsHeader bool `db:"is_header" json:"-"`
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
	// WorkerExists reports whether a worker with the given client_name has an
	// active session. Used by commands to warn on stderr before Start/Stop (P3-4 / §9).
	WorkerExists(ctx context.Context, worker string) bool
	StartChain(ctx context.Context, chainID int, worker string, delaySeconds int) error
	StopChain(ctx context.Context, chainID int, worker string) error
	PauseChain(ctx context.Context, ref string) error
	ResumeChain(ctx context.Context, ref string) error

	// --- CRUD & YAML (Phase 4) ---
	CreateChain(ctx context.Context, spec ChainSpec) (int, error)
	EditChain(ctx context.Context, ref string, spec ChainEdit) error
	DeleteChain(ctx context.Context, ref string) error
	AddTask(ctx context.Context, chainRef string, spec TaskSpec) (int, error)
	EditTask(ctx context.Context, taskID int, spec TaskEdit) error
	DeleteTask(ctx context.Context, taskID int) error
	MoveTask(ctx context.Context, taskID int, up bool) error
	ApplyYAML(ctx context.Context, path string, replace bool) (int, error)
	ExportYAML(ctx context.Context, refs []string) ([]byte, []string, error)

	// --- Log tail (Phase 5) ---

	// TailLogs streams log entries to out as they appear in timetable.log,
	// filtered by f.ChainID and f.ClientName. It blocks until ctx is cancelled.
	// Each new entry is passed to the emit callback; the caller formats/prints it.
	// (REQ-013 / P5-1)
	TailLogs(ctx context.Context, f LogFilter, emit func(LogEntry)) error

	// ListActivity returns a unified chronological feed from timetable.log and
	// timetable.execution_log, optionally filtered by chain and client.
	ListActivity(ctx context.Context, f LogFilter) ([]ActivityEntry, error)

	// ListActivityTree returns the unified feed grouped by chain run (chain +
	// client + virtual transaction id) for the `log list -o tree` view. The SQL
	// does all grouping/ordering via window functions: rows arrive run-by-run,
	// each run's "Starting chain" line first (marked IsHeader), runs ordered
	// newest-first by their latest activity. Rows without a chain sort last.
	ListActivityTree(ctx context.Context, f LogFilter) ([]ActivityEntry, error)

	// TailActivity streams the unified activity feed live. It blocks until ctx
	// is cancelled, polling both timetable.log and timetable.execution_log.
	TailActivity(ctx context.Context, f LogFilter, emit func(ActivityEntry)) error

	// ListRuns returns recent execution runs for a chain (one row per txid) (P5-4).
	ListRuns(ctx context.Context, ref string, limit int) ([]RunSummary, error)
	// ShowRun returns per-task detail rows for a single txid (P5-5).
	ShowRun(ctx context.Context, txid int64) ([]RunTaskDetail, error)
}

// RunSummary is one chain execution (one txid) from timetable.execution_log (P5-4).
type RunSummary struct {
	Txid        int64  `db:"txid" json:"txid"`
	StartedAt   string `db:"started_at" json:"started_at"`
	FinishedAt  string `db:"finished_at" json:"finished_at"`
	DurationMS  int64  `db:"duration_ms" json:"duration_ms"`
	Status      string `db:"status" json:"status"`
	ClientName  string `db:"client_name" json:"client_name"`
	TotalTasks  int    `db:"total_tasks" json:"total_tasks"`
	FailedTasks int    `db:"failed_tasks" json:"failed_tasks"`
}

// RunTaskDetail is one task row within a single txid (P5-5).
type RunTaskDetail struct {
	TaskID      int64  `db:"task_id" json:"task_id"`
	Kind        string `db:"kind" json:"kind"`
	Command     string `db:"command" json:"command"`
	StartedAt   string `db:"started_at" json:"started_at"`
	FinishedAt  string `db:"finished_at" json:"finished_at"`
	DurationMS  int64  `db:"duration_ms" json:"duration_ms"`
	Returncode  int    `db:"returncode" json:"returncode"`
	IgnoreError bool   `db:"ignore_error" json:"ignore_error"`
	Params      string `db:"params" json:"params"`
	Output      string `db:"output" json:"output"`
}

// ChainSpec describes a new chain (one initial task) for CreateChain.
type ChainSpec struct {
	Name         string
	Schedule     string
	Command      string
	Kind         string
	ClientName   string
	MaxInstances int
	Live         bool
	SelfDestruct bool
	Exclusive    bool
	OnError      string
}

// ChainEdit holds optional chain attribute updates; nil fields are left unchanged.
type ChainEdit struct {
	Schedule     *string
	ClientName   *string
	MaxInstances *int
	Live         *bool
	SelfDestruct *bool
	Exclusive    *bool
	OnError      *string
}

// TaskSpec describes a new task appended to a chain.
type TaskSpec struct {
	Name        string
	Kind        string
	Command     string
	RunAs       string
	ConnectStr  string
	IgnoreError bool
	Autonomous  bool
	Timeout     int
}

// TaskEdit holds optional task attribute updates; nil fields are left unchanged.
type TaskEdit struct {
	Name        *string
	Kind        *string
	Command     *string
	RunAs       *string
	ConnectStr  *string
	IgnoreError *bool
	Autonomous  *bool
	Timeout     *int
}
