package client

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Sentinel errors so commands and tests can react to schema problems (AC-009).
var (
	// ErrSchemaAbsent indicates the timetable.* schema does not exist.
	ErrSchemaAbsent = errors.New("timetable schema not found")
	// ErrSchemaIncompatible indicates the database schema version differs from
	// the version pgtt was built for.
	ErrSchemaIncompatible = errors.New("incompatible timetable schema version")
)

// PgClient is the pgxpool-backed implementation of Client.
type PgClient struct {
	pool *pgxpool.Pool
	// wantVersion is the schema version pgtt is compatible with, e.g. "00797".
	wantVersion string
}

// New returns a PgClient compatible with the given schema version.
func New(wantVersion string) *PgClient {
	return &PgClient{wantVersion: wantVersion}
}

// Connect opens a connection pool. It never creates or upgrades the schema
// (REQ-016): it only opens a pool and pings the server.
func (c *PgClient) Connect(ctx context.Context, dsn string) error {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		// ParseConfig errors may embed the DSN (and thus a password); never surface it.
		return errors.New("invalid connection string")
	}
	cfg.ConnConfig.RuntimeParams["application_name"] = "pgtt"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("opening connection pool: %w", redactDSNError(err, dsn))
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return fmt.Errorf("connecting to database: %w", redactDSNError(err, dsn))
	}
	c.pool = pool
	return nil
}

// Close releases the connection pool.
func (c *PgClient) Close() {
	if c.pool != nil {
		c.pool.Close()
		c.pool = nil
	}
}

// CheckSchemaVersion verifies the timetable.* schema exists and its latest
// migration version matches the version pgtt was built for (REQ-016 / AC-009).
//
// The schema version is the leading numeric token of the most recent row in
// timetable.migration (see internal/pgengine/sql/init.sql).
func (c *PgClient) CheckSchemaVersion(ctx context.Context) error {
	const q = `SELECT version FROM timetable.migration ORDER BY id DESC LIMIT 1`
	var version string
	if err := c.pool.QueryRow(ctx, q).Scan(&version); err != nil {
		// Missing schema/table surfaces as undefined_table (42P01) or
		// invalid_schema_name (3F000); treat any failure to read the migration
		// table as "schema absent" for a clear, actionable message.
		return ErrSchemaAbsent
	}
	got := schemaVersionToken(version)
	if got != c.wantVersion {
		return fmt.Errorf("%w: database has %q, pgtt requires %q",
			ErrSchemaIncompatible, got, c.wantVersion)
	}
	return nil
}

// schemaVersionToken extracts the leading numeric token from a migration
// version string, e.g. "00797 Add indexes" -> "00797".
func schemaVersionToken(version string) string {
	v := strings.TrimSpace(version)
	if i := strings.IndexByte(v, ' '); i >= 0 {
		return v[:i]
	}
	return v
}

// redactDSNError ensures a DSN (which may contain a password) never leaks
// through an error message (SEC-002 / AC-010).
func redactDSNError(err error, dsn string) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if dsn != "" && strings.Contains(msg, dsn) {
		return errors.New(strings.ReplaceAll(msg, dsn, "<dsn>"))
	}
	return err
}
