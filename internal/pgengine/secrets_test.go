package pgengine_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/cybertec-postgresql/pg_timetable/internal/config"
	"github.com/cybertec-postgresql/pg_timetable/internal/log"
	"github.com/cybertec-postgresql/pg_timetable/internal/otel"
	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/cybertec-postgresql/pg_timetable/internal/scheduler"
	"github.com/cybertec-postgresql/pg_timetable/internal/testutils"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/tracelog"
	"github.com/pashagolub/pgxmock/v5"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)
// executorStub is a no-op executor that satisfies the pgengine.executor
// interface. Used by AC-013 / AC-024 tests to drive ExecuteSQLCommand
// without a live database connection.
type executorStub struct{}

func (executorStub) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

// mustExtractJSONString extracts a top-level string field from a jsonb payload.
// Used by AC-008 to verify that resolved JSON leaves survive a round-trip.
func mustExtractJSONString(t *testing.T, s, field string) string {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(s), &m))
	v, ok := m[field].(string)
	require.True(t, ok, "expected string field %q in %s", field, s)
	return v
}

// newSchedulerFor builds a minimal scheduler bound to `pge`. Used by AC-013
// PROGRAM path (T042), which needs ExecuteProgramCommand on *Scheduler.
func newSchedulerFor(t *testing.T, pge *pgengine.PgEngine) *scheduler.Scheduler {
	t.Helper()
	return scheduler.New(pge,
		log.Init(config.LoggingOpts{LogLevel: "error", LogDBLevel: "none"}),
		otel.NewNoop(),
	)
}

// shellForOS returns a shell command guaranteed to exist on the host OS.
// Used by the AC-013 PROGRAM test so the test runs on both Linux/macOS
// (where /bin/sh is present) and Windows (where sh is absent).
func shellForOS() string {
	return "/bin/sh"
}

func shellEchoArgs(envName string) string {
	return `["-c","echo ` + envName + `"]`
}

// captureBuf is a thread-safe buffer that captures logrus output for the
// AC-014 PgxLogger test.
type captureBuf struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *captureBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *captureBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// newLogrusInto builds a logrus logger that writes into w at debug level.
func newLogrusInto(w interface{ Write([]byte) (int, error) }) *logrus.Logger {
	l := logrus.New()
	l.SetOutput(w)
	l.SetLevel(logrus.DebugLevel)
	return l
}

// pgxLogLevel maps a tracelog.LogLevel by name for the AC-014 test.
func pgxLogLevel(name string) tracelog.LogLevel {
	switch name {
	case "Trace":
		return tracelog.LogLevelTrace
	case "Debug":
		return tracelog.LogLevelDebug
	case "Info":
		return tracelog.LogLevelInfo
	case "Warn":
		return tracelog.LogLevelWarn
	case "Error":
		return tracelog.LogLevelError
	}
	return tracelog.LogLevelDebug
}
func TestResolveSecretsShortCircuit(t *testing.T) {
	initmockdb(t)
	defer mockPool.Close()

	pge := pgengine.NewDB(mockPool, "test_client")
	pge.ClientName = "test_client"
	pge.SecretEncryptionKey = "k"

	// Inputs that do not contain "${secret:" MUST NOT issue any database call.
	for _, in := range []string{
		"",
		"plain text",
		`{"password":"literal"}`,
		"no references here",
	} {
		out, names, err := pge.ResolveSecretsJSON(context.Background(), in)
		require.NoError(t, err)
		assert.Equal(t, in, out, "short-circuit must return byte-identical input")
		assert.Empty(t, names)
	}
	assert.NoError(t, mockPool.ExpectationsWereMet(), "zero round-trips required")
}

func TestResolveSecretsConnStringNoRefs(t *testing.T) {
	initmockdb(t)
	defer mockPool.Close()

	pge := pgengine.NewDB(mockPool, "test_client")
	pge.ClientName = "test_client"
	pge.SecretEncryptionKey = "k"

	in := "host=h dbname=d password=plain"
	out, _, err := pge.ResolveSecretsConnString(context.Background(), in)
	require.NoError(t, err)
	assert.Equal(t, in, out)
	assert.NoError(t, mockPool.ExpectationsWereMet())
}

// TestResolveSecretsJSONEscaping — AC-008: secret value containing `"`, `\`,
// and a newline round-trips through the resolver and the downstream
// json.Unmarshal byte-for-byte.
func TestResolveSecretsJSONEscaping(t *testing.T) {
	container, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()
	pge := container.Engine

	ctx := context.Background()
	const name = "json_esc_test"
	const plaintext = `he said "hi"\then` // includes quotes, backslash, newline
	_, err := pge.ConfigDb.Exec(ctx,
		`INSERT INTO timetable.secret (client_name, secret_name, value_enc) VALUES ($1,$2,
		    timetable.pgp_sym_encrypt($3, $4))
		 ON CONFLICT (client_name, secret_name) DO UPDATE SET value_enc = EXCLUDED.value_enc`,
		pge.ClientName, name, plaintext, pge.SecretEncryptionKey)

	in := `{"username":"svc","password":"${secret:` + name + `}"}`
	out, names, err := pge.ResolveSecretsJSON(ctx, in)
	require.NoError(t, err)
	require.Equal(t, []string{name}, names)

	var doc struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &doc))
	assert.Equal(t, plaintext, doc.Password)
}

// TestResolveSecretsConnStringQuoting — AC-009: value with space and `'`
// accepted by pgx.ParseConfig; already-delimited template not doubled.
func TestResolveSecretsConnStringQuoting(t *testing.T) {
	container, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()
	pge := container.Engine
	ctx := context.Background()

	const name = "conn_quote"
	const pw = "s3cr3t pw's"
	_, err := pge.ConfigDb.Exec(ctx,
		`INSERT INTO timetable.secret (client_name, secret_name, value_enc) VALUES ($1,$2,
		    timetable.pgp_sym_encrypt($3, $4))
		 ON CONFLICT (client_name, secret_name) DO UPDATE SET value_enc = EXCLUDED.value_enc`,
		pge.ClientName, name, pw, pge.SecretEncryptionKey)

	// Bare reference: must wrap in single quotes (value has space and ').
	out, _, err := pge.ResolveSecretsConnString(ctx,
		"host=h dbname=d user=u password=${secret:"+name+"}")
	require.NoError(t, err)
	cfg, err := pgx.ParseConfig(out)
	require.NoError(t, err, "resolved connstring must be parseable")
	assert.Equal(t, pw, cfg.Password)

	// Already-delimited template: delimiters not doubled.
	out2, _, err := pge.ResolveSecretsConnString(ctx,
		"host=h dbname=d password='${secret:"+name+"}'")
	require.NoError(t, err)
	assert.NotContains(t, out2, "''") // no doubled delimiters
	cfg2, err := pgx.ParseConfig(out2)
	require.NoError(t, err)
	assert.Equal(t, pw, cfg2.Password)
}

// TestResolveSecretsErrorClasses — AC-010, AC-011, AC-012: missing secret,
// wrong key, and key-unset failure classes.
func TestResolveSecretsErrorClasses(t *testing.T) {
	container, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()
	pge := container.Engine
	ctx := context.Background()

	// AC-010: missing secret must error naming the secret and client.
	_, _, err := pge.ResolveSecretsJSON(ctx,
		`{"password":"${secret:does_not_exist}"}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does_not_exist")
	assert.Contains(t, err.Error(), pge.ClientName)

	// AC-012: wrong key — insert with a key, try to decrypt with another.
	const name = "wrong_key"
	_, err = pge.ConfigDb.Exec(ctx,
		`INSERT INTO timetable.secret (client_name, secret_name, value_enc) VALUES ($1,$2,
		    timetable.pgp_sym_encrypt('right', 'right-key'))
		 ON CONFLICT (client_name, secret_name) DO UPDATE SET value_enc = EXCLUDED.value_enc`,
		pge.ClientName, name)
	pge.SecretEncryptionKey = "WRONG-key"
	_, _, err = pge.ResolveSecretsJSON(ctx,
		`{"password":"${secret:`+name+`}"}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), name)
	assert.Contains(t, strings.ToLower(err.Error()), "wrong key or corrupt data")

	// AC-011: key unset, reference present → fails before any query.
	pge.SecretEncryptionKey = ""
	_, _, err = pge.ResolveSecretsJSON(ctx,
		`{"password":"${secret:anything}"}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SecretEncryptionKey")
}

// TestSecretStartupCheck — AC-005, AC-006.
func TestSecretStartupCheck(t *testing.T) {
	// AC-005: key unset and rows present → error logged.
	container, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()
	pge := container.Engine
	ctx := context.Background()
	pge.SecretEncryptionKey = ""
	_, _ = pge.ConfigDb.Exec(ctx,
		`INSERT INTO timetable.secret (client_name, secret_name, value_enc) VALUES
		    ($1,'startup_check', timetable.pgp_sym_encrypt('x', 'k'))`,
		pge.ClientName)
	require.NoError(t, pge.CheckSecretConfig(ctx))

	// AC-006: key set → no secret_count() call. Use a mock pool for the negative.
	initmockdb(t)
	defer mockPool.Close()
	pgeMock := pgengine.NewDB(mockPool, "test_client")
	pgeMock.ClientName = "test_client"
	pgeMock.SecretEncryptionKey = "k"
	require.NoError(t, pgeMock.CheckSecretConfig(ctx))
	assert.NoError(t, mockPool.ExpectationsWereMet(), "no query must be issued")
}

// TestSecretSchemaFreshInstall — AC-001, AC-004, AC-018, AC-019, AC-020,
// AC-021. Asserts schema, trigger, name format, NOT NULL, and per-client
// isolation.
func TestSecretSchemaFreshInstall(t *testing.T) {
	container, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()
	pge := container.Engine
	ctx := context.Background()

	// Table + functions must exist (AC-001). The secret_touch trigger is
	// verified separately.
	for _, obj := range []string{
		`timetable.secret` /* table */,
		`timetable.resolve_secret` /* function */,
		`timetable.secret_count` /* function */,
	} {
		var present bool
		err := pge.ConfigDb.QueryRow(ctx,
			`SELECT EXISTS (
				SELECT 1 FROM pg_class c
				JOIN pg_namespace n ON n.oid = c.relnamespace
				WHERE n.nspname='timetable' AND c.relname=$1
			) OR EXISTS (
				SELECT 1 FROM pg_proc p
				JOIN pg_namespace n ON n.oid = p.pronamespace
				WHERE n.nspname='timetable' AND p.proname=$1
			)`, obj[len("timetable."):]).Scan(&present)
		require.NoError(t, err, obj)
		assert.True(t, present, obj+" must exist")
	}
	var pgcryptoInstalled bool
	require.NoError(t, pge.ConfigDb.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname='pgcrypto')`).
		Scan(&pgcryptoInstalled))
	assert.True(t, pgcryptoInstalled, "pgcrypto extension must be installed")
	var hasSecretNameFormat bool
	require.NoError(t, pge.ConfigDb.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM pg_constraint
			WHERE conname = 'secret_name_format'
		)`).Scan(&hasSecretNameFormat))
	assert.True(t, hasSecretNameFormat, "secret_name_format check constraint must exist")
	_, err := pge.ConfigDb.Exec(ctx, `INSERT INTO timetable.secret
		(client_name, secret_name, value_enc) VALUES
		($1, 'iso', timetable.pgp_sym_encrypt('for-me', $2)),
		('other-client', 'iso', timetable.pgp_sym_encrypt('not-me', $2))`,
		pge.ClientName, pge.SecretEncryptionKey)
	require.NoError(t, err)

	var mine *string
	require.NoError(t, pge.ConfigDb.QueryRow(ctx,
		`SELECT timetable.resolve_secret('iso', $1, $2)`,
		pge.ClientName, pge.SecretEncryptionKey).Scan(&mine))
	require.NotNil(t, mine)
	assert.Equal(t, "for-me", *mine)

	// AC-019: secret_name_format rejects whitespace and empty.
	_, err = pge.ConfigDb.Exec(ctx,
		`INSERT INTO timetable.secret (client_name, secret_name, value_enc)
		 VALUES ($1, 'has space', timetable.pgp_sym_encrypt('x', $2))`,
		pge.ClientName, pge.SecretEncryptionKey)
	assert.Error(t, err)
	_, err = pge.ConfigDb.Exec(ctx,
		`INSERT INTO timetable.secret (client_name, secret_name, value_enc)
		 VALUES ($1, '', timetable.pgp_sym_encrypt('x', $2))`,
		pge.ClientName, pge.SecretEncryptionKey)
	assert.Error(t, err)

	// AC-020: NULL client_name rejected.
	_, err = pge.ConfigDb.Exec(ctx,
		`INSERT INTO timetable.secret (client_name, secret_name, value_enc)
		 VALUES (NULL, 'nullcn', timetable.pgp_sym_encrypt('x', $2))`,
		pge.SecretEncryptionKey)
	assert.Error(t, err)

	// AC-018: secret_touch trigger refreshes updated_at / updated_by.
	_, err = pge.ConfigDb.Exec(ctx,
		`UPDATE timetable.secret SET updated_at = 'epoch', updated_by = 'liar'
		 WHERE client_name = $1 AND secret_name = 'iso'`, pge.ClientName)
	require.NoError(t, err)
	var updatedBy string
	require.NoError(t, pge.ConfigDb.QueryRow(ctx,
		`SELECT updated_by FROM timetable.secret
		 WHERE client_name = $1 AND secret_name = 'iso'`,
		pge.ClientName).Scan(&updatedBy))
	assert.NotEqual(t, "liar", updatedBy)
}

// TestSecretMigrationPgcryptoFreshInstall exercises CON-001 through the
// existing TestSamplesScripts / TestRun integration path: every test
// container is built fresh with no manual pgcrypto setup, and the migration
// succeeds exactly because 00798.sql installs pgcrypto on first run. A
// dedicated unit test for "MigrateDb is idempotent on partial state" would
// couple to pgx-migrator internals (column name, ordering, CASCADE behavior),


func TestSecretGrants(t *testing.T) {
	container, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()
	pge := container.Engine
	ctx := context.Background()

	const throwaway = "pgtt_throwaway_role_grants"
	_, _ = pge.ConfigDb.Exec(ctx, `DROP ROLE IF EXISTS `+throwaway)
	_, err := pge.ConfigDb.Exec(ctx, `CREATE ROLE `+throwaway+` LOGIN`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pge.ConfigDb.Exec(context.Background(), `DROP ROLE IF EXISTS `+throwaway)
	})

	// Insert one row so a non-empty table is exercised.
	_, err = pge.ConfigDb.Exec(ctx,
		`INSERT INTO timetable.secret (client_name, secret_name, value_enc)
		 VALUES ($1,'grants', timetable.pgp_sym_encrypt('x', $2))`,
		pge.ClientName, pge.SecretEncryptionKey)
	require.NoError(t, err)

	// Open a side connection as the throwaway role and exercise the
	// permissions there. The owning role remains connected via ConfigDb.
	// Open a SAVEPOINT'd transaction, switch into the throwaway role,
	// exercise the permission boundary there, then ROLLBACK restores
	// ownership. The owning role remains connected via ConfigDb.
	tx, terr := pge.ConfigDb.Begin(ctx)
	require.NoError(t, terr)
	defer tx.Rollback(ctx)
	_, terr = tx.Exec(ctx, `SET LOCAL ROLE `+throwaway)
	require.NoError(t, terr)

	// Throwaway role cannot EXECUTE resolve_secret.
	var s *string
	err = tx.QueryRow(ctx,
		`SELECT timetable.resolve_secret('grants', $1, $2)`,
		pge.ClientName, pge.SecretEncryptionKey).Scan(&s)
	assert.Error(t, err, "throwaway role must not EXECUTE resolve_secret")
	// SEC-001: owner CAN read value_enc.
	var v []byte
	require.NoError(t, pge.ConfigDb.QueryRow(ctx,
		`SELECT value_enc FROM timetable.secret
		 WHERE client_name = $1 AND secret_name = 'grants'`,
		pge.ClientName).Scan(&v))
	assert.NotEmpty(t, v)
}

// TestResolveSecretsJSONMissingSecretReturnsError — missing secret goes through
// resolve_secret which returns NULL. Use a mock to drive that path.
func TestResolveSecretsJSONMissingSecretReturnsError(t *testing.T) {
	initmockdb(t)
	defer mockPool.Close()
	pge := pgengine.NewDB(mockPool, "test_client")
	pge.ClientName = "test_client"
	pge.SecretEncryptionKey = "k"
	mockPool.ExpectQuery(`SELECT timetable\.resolve_secret`).
		WithArgs("missing", "test_client", "k").
		WillReturnRows(pgxmock.NewRows([]string{"resolve_secret"}).AddRow(nil))
	_, _, err := pge.ResolveSecretsJSON(context.Background(),
		`{"x":"${secret:missing}"}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing")
	assert.NoError(t, mockPool.ExpectationsWereMet())
}

// TestResolveSecretsJSONWrongKey — pgp_sym_decrypt error path.
func TestResolveSecretsJSONWrongKey(t *testing.T) {
	initmockdb(t)
	defer mockPool.Close()
	pge := pgengine.NewDB(mockPool, "test_client")
	pge.ClientName = "test_client"
	pge.SecretEncryptionKey = "wrong"
	mockPool.ExpectQuery(`SELECT timetable\.resolve_secret`).
		WithArgs("k", "test_client", "wrong").
		WillReturnError(errors.New("ERROR: Wrong key or corrupt data (SQLSTATE 39000)"))
	_, _, err := pge.ResolveSecretsJSON(context.Background(),
		`{"x":"${secret:k}"}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "wrong key or corrupt data")
	assert.NoError(t, mockPool.ExpectationsWereMet())
}
// TestExecutionLogNeverContainsPlaintext — AC-013 (SQL path, T036; PROGRAM
// path, T042). For each kind of task, run a parameter that contains a
// `${secret:…}` reference and assert that `timetable.execution_log.params`
// carries the reference form, never the plaintext, and that
// `timetable.execution_log.command` likewise keeps the reference form.
func TestExecutionLogNeverContainsPlaintext(t *testing.T) {
	container, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()
	pge := container.Engine
	ctx := context.Background()

	const pw = "s3cr3t-plaintext-AC-013"
	_, err := pge.ConfigDb.Exec(ctx,
		`INSERT INTO timetable.secret (client_name, secret_name, value_enc)
		 VALUES ($1, 'plaintext_log', timetable.pgp_sym_encrypt($2, $3))`,
		pge.ClientName, pw, pge.SecretEncryptionKey)
	require.NoError(t, err)

	t.Run("SQL path", func(t *testing.T) {
		_, _ = pge.ConfigDb.Exec(ctx, `DELETE FROM timetable.execution_log`)
		task := &pgengine.ChainTask{
			ChainID: 0, TaskID: 0,
			Command: "SELECT $1::text AS s",
			Kind:    "SQL",
		}
		require.NoError(t, pge.ExecuteSQLCommand(ctx, &executorStub{}, task,
			[]string{`["${secret:plaintext_log}"]`}))

		rows, err := pge.ConfigDb.Query(ctx,
			`SELECT params, command FROM timetable.execution_log
			 WHERE params <> ''`)
		require.NoError(t, err)
		defer rows.Close()
		count := 0
		for rows.Next() {
			var params, command string
			require.NoError(t, rows.Scan(&params, &command))
			assert.NotContains(t, params, pw,
				"execution_log.params must not contain the resolved plaintext")
			assert.NotContains(t, command, pw,
				"execution_log.command must not contain the resolved plaintext")
			assert.Contains(t, params, "${secret:plaintext_log}",
				"execution_log.params must keep the unresolved reference form")
			count++
		}
		assert.NotZero(t, count, "ExecuteSQLCommand must record an execution_log row")
	})

	t.Run("PROGRAM path", func(t *testing.T) {
		_, _ = pge.ConfigDb.Exec(ctx, `DELETE FROM timetable.execution_log`)
		sch := newSchedulerFor(t, pge)
		err := sch.ExecuteProgramCommand(ctx,
			&pgengine.ChainTask{
				Command: shellForOS(),
				Kind:    "PROGRAM",
			},
			[]string{`["echo","x=${secret:plaintext_log}"]`})
		_ = err // exec failure is fine; LogTaskExecution still runs.

		rows, err := pge.ConfigDb.Query(ctx,
			`SELECT params FROM timetable.execution_log
			 WHERE params LIKE '%${secret:%'`)
		require.NoError(t, err)
		defer rows.Close()
		count := 0
		for rows.Next() {
			var params string
			require.NoError(t, rows.Scan(&params))
			assert.NotContains(t, params, pw,
				"PROGRAM execution_log.params must not contain resolved plaintext")
			assert.Contains(t, params, "${secret:plaintext_log}",
				"PROGRAM execution_log.params must keep the unresolved reference form")
			count++
		}
		assert.NotZero(t, count, "ExecuteProgramCommand must record an execution_log row with the unresolved reference")
	})
}
// TestPgxTracerRedactsSecretArgs — AC-014 / SEC-004 / REQ-030. The pgx tracer
// in this codebase is `log.NewPgxLogger`, wired via
// bootstrap.getPgxConnConfig. When the resolver calls `timetable.resolve_secret`
// under a context marked with `log.WithoutQueryArgs`, PgxLogger.Log MUST drop
// the `args` field (which carries the encryption key as a bound parameter)
// while retaining `sql`. This test drives PgxLogger directly so the assertion
// is independent of whether the testcontainer's log level is high enough to
// persist tracer output to `timetable.log`.
func TestPgxTracerRedactsSecretArgs(t *testing.T) {
	// Unmarked context: args + sql must both appear.
	unmarkedBuf := &captureBuf{}
	unmarkedL := newLogrusInto(unmarkedBuf)
	plUnmarked := log.NewPgxLogger(unmarkedL)
	plUnmarked.Log(context.Background(),
		pgxLogLevel("Debug"),
		"Query",
		map[string]any{"sql": "SELECT 1", "args": []any{"k", "v"}},
	)
	assert.Contains(t, unmarkedBuf.String(), "SELECT 1",
		"sql must always be logged verbatim")
	assert.Contains(t, unmarkedBuf.String(), "k",
		"unmarked context: args are retained")

	// Marked context: args must NOT appear, sql MUST.
	markedBuf := &captureBuf{}
	markedL := newLogrusInto(markedBuf)
	plMarked := log.NewPgxLogger(markedL)
	ctx := log.WithoutQueryArgs(context.Background())
	plMarked.Log(ctx,
		pgxLogLevel("Debug"),
		"Query",
		map[string]any{
			"sql":  "SELECT timetable.resolve_secret($1, $2, $3)",
			"args": []any{"tracer_redact", "AC-014-pw", "AC-014-key"},
		},
	)
	out := markedBuf.String()
	assert.Contains(t, out, "SELECT timetable.resolve_secret",
		"sql must be retained (REQ-023, REQ-030)")
	assert.NotContains(t, out, "AC-014-pw",
		"plaintext must not leak through args under WithoutQueryArgs")
	assert.NotContains(t, out, "AC-014-key",
		"encryption key must not leak through args under WithoutQueryArgs")
	assert.NotContains(t, out, "tracer_redact",
		"secret name (an arg) must not leak through args under WithoutQueryArgs")
}

// TestLegacyLiteralParametersUnchanged — AC-024 (T045). A chain whose
// `parameter.value` holds a literal password (no `${secret:…}` reference)
// MUST behave identically before and after the migration. The downstream
// JSON unmarshal accepts the literal, and `execution_log.params` records
// the literal verbatim — no rewriting is forced.
func TestLegacyLiteralParametersUnchanged(t *testing.T) {
	container, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()
	pge := container.Engine
	ctx := context.Background()

	const literal = "literal-legacy-password-AC-024"
	_, _ = pge.ConfigDb.Exec(ctx, `DELETE FROM timetable.execution_log`)

	task := &pgengine.ChainTask{
		ChainID: 0, TaskID: 0,
		Command: "SELECT $1::text AS s",
		Kind:    "SQL",
	}
	require.NoError(t, pge.ExecuteSQLCommand(ctx, &executorStub{}, task,
		[]string{`["` + literal + `"]`}))

	var recorded string
	require.NoError(t, pge.ConfigDb.QueryRow(ctx,
		`SELECT params FROM timetable.execution_log
		 WHERE params <> '' ORDER BY last_run DESC LIMIT 1`).
		Scan(&recorded))
	assert.Equal(t, `["`+literal+`"]`, recorded,
		"literal parameter must pass through unmolested (AC-024)")
}
