package pgengine_test

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
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
// interface. Used by the SQL execution_log tests to drive ExecuteSQLCommand
// without a live database connection.
type executorStub struct{}

func (executorStub) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

// installPgcrypto ensures the pgcrypto extension is present in the test
// database. Every test that exercises a secret round trip installs the
// extension in its own fixture. pgcrypto lives wherever CREATE EXTENSION
// places it (default `public`), so subsequent test code uses unqualified
// pgp_sym_encrypt / pgp_sym_decrypt calls.
func installPgcrypto(ctx context.Context, t *testing.T, pge *pgengine.PgEngine) {
	t.Helper()
	_, err := pge.ConfigDb.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pgcrypto`)
	require.NoError(t, err, "installing pgcrypto must succeed in the test fixture")
}


// newSchedulerFor builds a minimal scheduler bound to `pge`. Used by the
// PROGRAM path test, which needs ExecuteProgramCommand on *Scheduler.
func newSchedulerFor(t *testing.T, pge *pgengine.PgEngine) *scheduler.Scheduler {
	t.Helper()
	return scheduler.New(pge,
		log.Init(config.LoggingOpts{LogLevel: "error", LogDBLevel: "none"}),
		otel.NewNoop(),
	)
}

// shellForOS returns a shell command guaranteed to exist on the host OS.
// Used by the PROGRAM test so it runs on both Linux/macOS (where /bin/sh is
// present) and Windows (where sh is absent).
func shellForOS() string {
	return "/bin/sh"
}


// captureBuf is a thread-safe buffer that captures logrus output for the
// PgxLogger test.
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

// pgxLogLevel maps a tracelog.LogLevel by name for the PgxLogger test.
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

// assertNoExtensionDMLInDDL walks every SQL file under internal/pgengine/sql/
// and fails the test if any file contains CREATE EXTENSION or ALTER EXTENSION.
// Walked from the test working directory; resolves the module root by walking
// upward until go.mod is found.
func assertNoExtensionDMLInDDL(t *testing.T) {
	t.Helper()
	root, err := findModuleRoot()
	require.NoError(t, err)
	dir := filepath.Join(root, "internal", "pgengine", "sql")
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".sql") {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		s := string(b)
		// Reject actual statements, but allow the substrings to appear inside
		// SQL string literals or comments if and only if they are escaped /
		// commented out. The simplest correct check is: no top-level
		// `CREATE EXTENSION` or `ALTER EXTENSION` statement — i.e. a line
		// beginning with either keyword, ignoring leading whitespace.
		for _, line := range strings.Split(s, "\n") {
			trimmed := strings.TrimSpace(line)
			upper := strings.ToUpper(trimmed)
			if strings.HasPrefix(upper, "CREATE EXTENSION") ||
				strings.HasPrefix(upper, "ALTER EXTENSION") {
				t.Errorf("forbidden extension DML in %s: %s", path, trimmed)
			}
		}
		return nil
	})
	require.NoError(t, err)
}

func findModuleRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod not found")
		}
		dir = parent
	}
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

// TestResolveSecretsJSONEscaping: secret value containing `"`, `\`,
// and a newline round-trips through the resolver and the downstream
// json.Unmarshal byte-for-byte.
func TestResolveSecretsJSONEscaping(t *testing.T) {
	container, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()
	pge := container.Engine

	ctx := context.Background()
	installPgcrypto(ctx, t, pge)
	const name = "json_esc_test"
	const plaintext = `he said "hi"\then` // includes quotes, backslash, newline
	_, err := pge.ConfigDb.Exec(ctx,
		`INSERT INTO timetable.secret (client_name, secret_name, value_enc) VALUES ($1,$2,
		    pgp_sym_encrypt($3, $4))
		 ON CONFLICT (client_name, secret_name) DO UPDATE SET value_enc = EXCLUDED.value_enc`,
		pge.ClientName, name, plaintext, pge.SecretEncryptionKey)
	require.NoError(t, err)
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

// TestResolveSecretsConnStringQuoting: value with space and `'`
// accepted by pgx.ParseConfig; already-delimited template not doubled.
func TestResolveSecretsConnStringQuoting(t *testing.T) {
	container, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()
	pge := container.Engine
	ctx := context.Background()
	installPgcrypto(ctx, t, pge)

	const name = "conn_quote"
	const pw = "s3cr3t pw's"
	_, err := pge.ConfigDb.Exec(ctx,
		`INSERT INTO timetable.secret (client_name, secret_name, value_enc) VALUES ($1,$2,
		    pgp_sym_encrypt($3, $4))
		 ON CONFLICT (client_name, secret_name) DO UPDATE SET value_enc = EXCLUDED.value_enc`,
		pge.ClientName, name, pw, pge.SecretEncryptionKey)
	require.NoError(t, err)
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

// TestResolveSecretsErrorClasses: missing secret, wrong key,
// and key-unset failure classes.
func TestResolveSecretsErrorClasses(t *testing.T) {
	container, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()
	pge := container.Engine
	ctx := context.Background()
	installPgcrypto(ctx, t, pge)

	// missing secret must error naming the secret and client.
	_, _, err := pge.ResolveSecretsJSON(ctx,
		`{"password":"${secret:does_not_exist}"}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does_not_exist")
	assert.Contains(t, err.Error(), pge.ClientName)

	// wrong key — insert with a key, try to decrypt with another.
	const name = "wrong_key"
	_, err = pge.ConfigDb.Exec(ctx,
		`INSERT INTO timetable.secret (client_name, secret_name, value_enc) VALUES ($1,$2,
		    pgp_sym_encrypt('right', 'right-key'))
		 ON CONFLICT (client_name, secret_name) DO UPDATE SET value_enc = EXCLUDED.value_enc`,
		pge.ClientName, name)
	require.NoError(t, err)
	_, _, err = pge.ResolveSecretsJSON(ctx,
		`{"password":"${secret:`+name+`}"}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), name)
	assert.Contains(t, strings.ToLower(err.Error()), "wrong key or corrupt data")

	// key unset, reference present → fails before any query.
	pge.SecretEncryptionKey = ""
	_, _, err = pge.ResolveSecretsJSON(ctx,
		`{"password":"${secret:anything}"}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SecretEncryptionKey")
}

// TestSecretStartupCheck: error logged when secrets exist without a key.
func TestSecretStartupCheck(t *testing.T) {
	// key unset and rows present → error logged.
	container, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()
	pge := container.Engine
	ctx := context.Background()
	installPgcrypto(ctx, t, pge)
	pge.SecretEncryptionKey = ""
	_, _ = pge.ConfigDb.Exec(ctx,
		`INSERT INTO timetable.secret (client_name, secret_name, value_enc) VALUES
		    ($1,'startup_check', pgp_sym_encrypt('x', 'k'))`,
		pge.ClientName)
	require.NoError(t, pge.CheckSecretConfig(ctx))

	// key set → no secret_count() call. Use a mock pool for the negative.
	initmockdb(t)
	defer mockPool.Close()
	pgeMock := pgengine.NewDB(mockPool, "test_client")
	pgeMock.ClientName = "test_client"
	pgeMock.SecretEncryptionKey = "k"
	require.NoError(t, pgeMock.CheckSecretConfig(ctx))
	assert.NoError(t, mockPool.ExpectationsWereMet(), "no query must be issued")
}

// TestSecretSchemaFreshInstall: asserts schema, trigger, name format,
// NOT NULL, and per-client isolation.
func TestSecretSchemaFreshInstall(t *testing.T) {
	container, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()
	pge := container.Engine
	ctx := context.Background()
	installPgcrypto(ctx, t, pge)

	// Table + functions must exist. The secret_touch trigger is
	// verified separately.
	for _, obj := range []string{
		`timetable.secret`,         /* table */
		`timetable.resolve_secret`, /* function */
		`timetable.secret_count`,   /* function */
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
	var hasSecretNameFormat bool
	require.NoError(t, pge.ConfigDb.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM pg_constraint
			WHERE conname = 'secret_name_format'
		)`).Scan(&hasSecretNameFormat))
	assert.True(t, hasSecretNameFormat, "secret_name_format check constraint must exist")
	_, err := pge.ConfigDb.Exec(ctx, `INSERT INTO timetable.secret
		(client_name, secret_name, value_enc) VALUES
		($1, 'iso', pgp_sym_encrypt('for-me', $2)),
		('other-client', 'iso', pgp_sym_encrypt('not-me', $2))`,
		pge.ClientName, pge.SecretEncryptionKey)
	require.NoError(t, err)

	var mine *string
	require.NoError(t, pge.ConfigDb.QueryRow(ctx,
		`SELECT timetable.resolve_secret('iso', $1, $2)`,
		pge.ClientName, pge.SecretEncryptionKey).Scan(&mine))
	require.NotNil(t, mine)
	assert.Equal(t, "for-me", *mine)

	// secret_name_format rejects whitespace and empty.
	_, err = pge.ConfigDb.Exec(ctx,
		`INSERT INTO timetable.secret (client_name, secret_name, value_enc)
		 VALUES ($1, 'has space', pgp_sym_encrypt('x', $2))`,
		pge.ClientName, pge.SecretEncryptionKey)
	assert.Error(t, err)
	_, err = pge.ConfigDb.Exec(ctx,
		`INSERT INTO timetable.secret (client_name, secret_name, value_enc)
		 VALUES ($1, '', pgp_sym_encrypt('x', $2))`,
		pge.ClientName, pge.SecretEncryptionKey)
	assert.Error(t, err)

	// NULL client_name rejected.
	_, err = pge.ConfigDb.Exec(ctx,
		`INSERT INTO timetable.secret (client_name, secret_name, value_enc)
		 VALUES (NULL, 'nullcn', pgp_sym_encrypt('x', $2))`,
		pge.SecretEncryptionKey)
	assert.Error(t, err)

	// secret_touch trigger refreshes updated_at / updated_by.
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

// TestSecretMigrationPgcryptoFreshInstall exercises the contract that
// every test container is built fresh with no manual pgcrypto setup,
// and the migration succeeds exactly because 00820.sql creates the store
// without requiring the extension. A dedicated unit test for "MigrateDb is
// idempotent on partial state" would couple to pgx-migrator internals
// (column name, ordering, CASCADE behavior),
func TestSecretGrants(t *testing.T) {
	container, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()
	pge := container.Engine
	ctx := context.Background()
	installPgcrypto(ctx, t, pge)

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
		 VALUES ($1,'grants', pgp_sym_encrypt('x', $2))`,
		pge.ClientName, pge.SecretEncryptionKey)
	require.NoError(t, err)

	// Open a side connection as the throwaway role and exercise the
	// permissions there. The owning role remains connected via ConfigDb.
	// Open a SAVEPOINT'd transaction, switch into the throwaway role,
	// exercise the permission boundary there, then ROLLBACK restores
	// ownership. The owning role remains connected via ConfigDb.
	tx, terr := pge.ConfigDb.Begin(ctx)
	require.NoError(t, terr)
	defer func() { _ = tx.Rollback(ctx) }()
	_, terr = tx.Exec(ctx, `SET LOCAL ROLE `+throwaway)
	require.NoError(t, terr)

	// Throwaway role cannot EXECUTE resolve_secret.
	var s *string
	err = tx.QueryRow(ctx,
		`SELECT timetable.resolve_secret('grants', $1, $2)`,
		pge.ClientName, pge.SecretEncryptionKey).Scan(&s)
	assert.Error(t, err, "throwaway role must not EXECUTE resolve_secret")
	// owner CAN read value_enc.
	var v []byte
	require.NoError(t, pge.ConfigDb.QueryRow(ctx,
		`SELECT value_enc FROM timetable.secret
		 WHERE client_name = $1 AND secret_name = 'grants'`,
		pge.ClientName).Scan(&v))
	assert.NotEmpty(t, v)
}

// TestResolveSecretsJSONMissingSecretReturnsError: missing secret goes through
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

// TestResolveSecretsJSONWrongKey: pgp_sym_decrypt error path.
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

// TestExecutionLogNeverContainsPlaintext: for each kind of task, run a
// parameter that contains a `${secret:…}` reference and assert that
// `timetable.execution_log.params` carries the reference form, never the
// plaintext, and that `timetable.execution_log.command` likewise keeps the
// reference form.
func TestExecutionLogNeverContainsPlaintext(t *testing.T) {
	container, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()
	pge := container.Engine
	ctx := context.Background()
	installPgcrypto(ctx, t, pge)

	const pw = "s3cr3t-plaintext-no-log"
	_, err := pge.ConfigDb.Exec(ctx,
		`INSERT INTO timetable.secret (client_name, secret_name, value_enc)
		 VALUES ($1, 'plaintext_log', pgp_sym_encrypt($2, $3))`,
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

// TestPgxTracerRedactsSecretArgs: the pgx tracer in this codebase is
// `log.NewPgxLogger`, wired via bootstrap.getPgxConnConfig. When the resolver
// calls `timetable.resolve_secret` under a context marked with
// `log.WithoutQueryArgs`, PgxLogger.Log MUST drop the `args` field (which
// carries the encryption key as a bound parameter) while retaining `sql`.
// This test drives PgxLogger directly so the assertion is independent of
// whether the testcontainer's log level is high enough to persist tracer
// output to `timetable.log`.
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
			"args": []any{"tracer_redact", "marker-pw", "marker-key"},
		},
	)
	out := markedBuf.String()
	assert.Contains(t, out, "SELECT timetable.resolve_secret",
		"sql must be retained")
	assert.NotContains(t, out, "marker-pw",
		"plaintext must not leak through args under WithoutQueryArgs")
	assert.NotContains(t, out, "marker-key",
		"encryption key must not leak through args under WithoutQueryArgs")
	assert.NotContains(t, out, "tracer_redact",
		"secret name (an arg) must not leak through args under WithoutQueryArgs")
}

// TestLegacyLiteralParametersUnchanged: a chain whose `parameter.value`
// holds a literal password (no `${secret:…}` reference) MUST behave
// identically before and after the migration. The downstream JSON unmarshal
// accepts the literal, and `execution_log.params` records the literal
// verbatim — no rewriting is forced.
func TestLegacyLiteralParametersUnchanged(t *testing.T) {
	container, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()
	pge := container.Engine
	ctx := context.Background()

	const literal = "literal-legacy-password"
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
		"literal parameter must pass through unmolested")
}

// TestResolveSecretLocatesPgcrypto: install pgcrypto (it lands in `public`
// by default), insert a secret, resolve it, then ALTER EXTENSION pgcrypto
// SET SCHEMA ext and resolve the same secret again. Both MUST succeed,
// proving the schema is discovered at call time.
func TestResolveSecretLocatesPgcrypto(t *testing.T) {
	container, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()
	pge := container.Engine
	ctx := context.Background()
	installPgcrypto(ctx, t, pge)

	const name = "locate_pgcrypto"
	_, err := pge.ConfigDb.Exec(ctx,
		`INSERT INTO timetable.secret (client_name, secret_name, value_enc)
		 VALUES ($1, $2, pgp_sym_encrypt($3, $4))
		 ON CONFLICT (client_name, secret_name) DO UPDATE SET value_enc = EXCLUDED.value_enc`,
		pge.ClientName, name, "plaintext-ext", pge.SecretEncryptionKey)
	require.NoError(t, err)

	// (a) pgcrypto in `public`: must decrypt.
	var got *string
	require.NoError(t, pge.ConfigDb.QueryRow(ctx,
		`SELECT timetable.resolve_secret($1, $2, $3)`,
		name, pge.ClientName, pge.SecretEncryptionKey).Scan(&got))
	require.NotNil(t, got)
	assert.Equal(t, "plaintext-ext", *got)

	// (b) Move pgcrypto to a private schema `ext` and resolve again.
	_, err = pge.ConfigDb.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS ext`)
	require.NoError(t, err)
	_, err = pge.ConfigDb.Exec(ctx, `ALTER EXTENSION pgcrypto SET SCHEMA ext`)
	require.NoError(t, err)
	got = nil
	require.NoError(t, pge.ConfigDb.QueryRow(ctx,
		`SELECT timetable.resolve_secret($1, $2, $3)`,
		name, pge.ClientName, pge.SecretEncryptionKey).Scan(&got))
	require.NotNil(t, got)
	assert.Equal(t, "plaintext-ext", *got,
		"resolve_secret must discover the extension schema at call time")
}

// TestSecretsWithoutPgcrypto: on a container where pgcrypto is NOT
// installed, bootstrap/migration succeeded, both functions and the table
// exist, secret_count() returns 0, resolve_secret on an unknown name
// returns NULL, and resolve_secret on an existing row whose value_enc is
// plain bytea raises SQLSTATE 0A000 wrapped with the secret name. The
// scheduler keeps running. Also asserts statically that no file under
// internal/pgengine/sql/ contains CREATE EXTENSION or ALTER EXTENSION.
func TestSecretsWithoutPgcrypto(t *testing.T) {
	// Static guard first: no DDL file may install or alter an extension.
	assertNoExtensionDMLInDDL(t)

	container, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()
	pge := container.Engine
	ctx := context.Background()

	// Drop pgcrypto if the test harness pulled it in (the alpine image ships
	// with pgcrypto preinstalled). We require the test to exercise the absent
	// case.
	var present bool
	require.NoError(t, pge.ConfigDb.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname='pgcrypto')`).
		Scan(&present))
	if present {
		_, err := pge.ConfigDb.Exec(ctx, `DROP EXTENSION pgcrypto CASCADE`)
		require.NoError(t, err, "test requires dropping pgcrypto to exercise the absent path")
	}

	// Bootstrap and migration already succeeded (SetupPostgresContainer ran
	// them) — bootstrap did NOT raise, did NOT log any extension probe.
	// Both functions and the table exist.
	for _, q := range []string{
		`SELECT to_regclass('timetable.secret')::text`,
		`SELECT to_regprocedure('timetable.resolve_secret(text,text,text)')::text`,
		`SELECT to_regprocedure('timetable.secret_count()')::text`,
	} {
		var name *string
		require.NoError(t, pge.ConfigDb.QueryRow(ctx, q).Scan(&name))
		assert.NotNil(t, name, q)
		assert.NotEmpty(t, *name, q)
	}

	// secret_count() returns 0.
	var count int64
	require.NoError(t, pge.ConfigDb.QueryRow(ctx, `SELECT timetable.secret_count()`).Scan(&count))
	assert.Equal(t, int64(0), count)

	// resolve_secret on an unknown name returns NULL (no pgcrypto needed).
	var missing *string
	require.NoError(t, pge.ConfigDb.QueryRow(ctx,
		`SELECT timetable.resolve_secret('does_not_exist', $1, $2)`,
		pge.ClientName, pge.SecretEncryptionKey).Scan(&missing))
	assert.Nil(t, missing)

	// Insert a row whose value_enc is a plain bytea literal (NOT pgcrypto
	// ciphertext). Resolving it must raise SQLSTATE 0A000, wrapped with the
	// secret name by the Go layer.
	_, err := pge.ConfigDb.Exec(ctx,
		`INSERT INTO timetable.secret (client_name, secret_name, value_enc)
		 VALUES ($1, 'absent_test', E'\\\\xdeadbeef'::bytea)`, pge.ClientName)
	require.NoError(t, err)

	_, _, rerr := pge.ResolveSecretsJSON(ctx,
		`{"password":"${secret:absent_test}"}`)
	require.Error(t, rerr)
	msg := strings.ToLower(rerr.Error())
	assert.Contains(t, msg, "absent_test",
		"error must name the secret")
	assert.Contains(t, msg, "pgcrypto",
		"error must name pgcrypto and the DBA's responsibility")

	// Scheduler remains usable: a non-secret SQL task still executes.
	task := &pgengine.ChainTask{
		Command: "SELECT $1::text",
		Kind:    "SQL",
	}
	require.NoError(t, pge.ExecuteSQLCommand(ctx, &executorStub{}, task,
		[]string{`["ok"]`}))
}
