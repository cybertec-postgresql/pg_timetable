package scheduler

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/internal/config"
	"github.com/cybertec-postgresql/pg_timetable/internal/log"
	"github.com/cybertec-postgresql/pg_timetable/internal/otel"
	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/cybertec-postgresql/pg_timetable/internal/tasks"
	"github.com/cybertec-postgresql/pg_timetable/internal/testutils"
	gomail "github.com/ory/mail/v3"
	"github.com/pashagolub/pgxmock/v5"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// installPgcrypto ensures the pgcrypto extension is present in the test
// database. Every test that exercises a secret round trip installs the
// extension in its own fixture.
func installPgcrypto(ctx context.Context, t *testing.T, pge *pgengine.PgEngine) {
	t.Helper()
	_, err := pge.ConfigDb.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pgcrypto`)
	require.NoError(t, err, "installing pgcrypto must succeed in the test fixture")
}

func TestExecuteTask(t *testing.T) {
	mock, err := pgxmock.NewPool() //
	a := assert.New(t)
	a.NoError(err)
	pge := pgengine.NewDB(mock, "--log-database-level=none")
	mocksch := New(pge, log.Init(config.LoggingOpts{LogLevel: "panic", LogDBLevel: "none"}), otel.NewNoop())

	task := &pgengine.ChainTask{Command: "NoOp"}
	et := func(cmd string, params []string) (err error) {
		task.Command = cmd
		err = mocksch.executeBuiltinTask(context.TODO(), task, params)
		return
	}

	a.Error(et("foo", []string{}))

	a.Error(et("Sleep", []string{"foo"}))
	a.False(task.StartedAt.IsZero()) // must be set to current time for every new parameter
	a.NoError(et("Sleep", []string{"1"}))
	a.GreaterOrEqual(time.Since(task.StartedAt), time.Second)

	a.NoError(et("NoOp", []string{}))
	a.NoError(et("NoOp", []string{"foo", "bar"}))

	a.NoError(et("Log", []string{"foo"}))

	a.Error(et("CopyFromFile", []string{"foo"}), "Invalid json")
	a.Error(et("CopyFromFile", []string{`{"sql": "COPY", "filename": "foo"}`}), "Acquire() should fail")

	a.Error(et("CopyToFile", []string{"foo"}), "Invalid json")
	a.Error(et("CopyToFile", []string{`{"sql": "COPY", "filename": "foo"}`}), "Acquire() should fail")

	a.Error(et("CopyToProgram", []string{"foo"}), "Invalid json")
	a.Error(et("CopyToProgram", []string{`{"sql": "COPY", "program": "foo"}`}), "Acquire() should fail")

	a.Error(et("CopyFromProgram", []string{"foo"}), "Invalid json")
	a.Error(et("CopyFromProgram", []string{`{"sql": "COPY", "program": "foo"}`}), "Acquire() should fail")

	a.Error(et("SendMail", []string{"foo"}), "Invalid json")
	a.Error(et("SendMail", []string{`{"ServerHost":"smtp.example.com","ServerPort":587,"Username":"user"}`}))

	a.Error(et("Download", []string{"foo"}), "Invalid json")
	a.EqualError(et("Download", []string{`{"workersnum": 0, "fileurls": [] }`}),
		"files to download are not specified", "Download with empty files should fail")
	a.Error(et("Download", []string{`{"workersnum": 0, "fileurls": ["http://foo.bar"], "destpath": "" }`}),
		"Downlod incorrect url should fail")

	a.NoError(et("Shutdown", []string{}))
}

// fakeDialer implements tasks.Dialer and captures nothing itself; the
// password is captured by the tasks.NewDialer override in
// TestSendMailResolvesSecret.
type fakeDialer struct{}

func (fakeDialer) DialAndSend(context.Context, ...*gomail.Message) error { return nil }

// TestSendMailResolvesSecret stores a secret for the running client, calls
// taskSendMail (the actual code path executed by the scheduler) with a
// reference, and asserts that the *decrypted* plaintext — not the literal
// ${secret:...} reference — is what reaches the tasks.SendMail boundary.
// tasks.NewDialer is overridden to capture the password argument instead of
// performing a real SMTP dial, so a regression where taskSendMail stops
// calling the resolver (and passes the unresolved reference through) is
// caught here, unlike a test that exercises ResolveSecretsJSON directly.
func TestSendMailResolvesSecret(t *testing.T) {
	container, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()
	pge := container.Engine
	ctx := context.Background()
	installPgcrypto(ctx, t, pge)

	const name = "sendmail_resolve"
	const pw = "real-secret-pw"
	_, err := pge.ConfigDb.Exec(ctx,
		`INSERT INTO timetable.secret (client_name, secret_name, value_enc)
		 VALUES ($1, $2, pgp_sym_encrypt($3, $4))
		 ON CONFLICT (client_name, secret_name) DO UPDATE SET value_enc = EXCLUDED.value_enc`,
		pge.ClientName, name, pw, pge.SecretEncryptionKey)
	require.NoError(t, err)

	sch := New(pge, log.Init(config.LoggingOpts{LogLevel: "panic", LogDBLevel: "none"}), otel.NewNoop())

	var gotPassword string
	origNewDialer := tasks.NewDialer
	tasks.NewDialer = func(_ string, _ int, _, password string) tasks.Dialer {
		gotPassword = password
		return fakeDialer{}
	}
	defer func() { tasks.NewDialer = origNewDialer }()

	param := `{"serverhost":"127.0.0.1","serverport":1,"username":"u","senderaddr":"u@x","toaddr":["u@x"],"subject":"s","msgbody":"b","password":"${secret:` + name + `}"}`
	_, err = taskSendMail(ctx, sch, param)
	require.NoError(t, err)
	assert.Equal(t, pw, gotPassword, "taskSendMail must pass the decrypted secret, not the literal reference, to SendMail")
}

// TestBuiltinDebugLogOmitsParamValues — the debug log emitted by
// executeBuiltinTask must carry a parameter count and MUST NOT contain any
// parameter value.
func TestBuiltinDebugLogOmitsParamValues(t *testing.T) {
	var buf bytes.Buffer
	l := logrus.New()
	l.SetOutput(&buf)
	l.SetLevel(logrus.DebugLevel)

	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()
	pge := pgengine.NewDB(mock, "--clientname=worker")
	sch := New(pge, log.Init(config.LoggingOpts{LogLevel: "debug"}), otel.NewNoop())

	task := &pgengine.ChainTask{Command: "NoOp"}
	require.NoError(t, sch.executeBuiltinTask(
		log.WithLogger(context.Background(), l), task,
		[]string{"super-secret-password", "another-secret"}))

	out := buf.String()
	assert.Contains(t, out, "Executing builtin task")
	assert.Contains(t, out, "param_count")
	assert.NotContains(t, out, "super-secret-password")
	assert.NotContains(t, out, "another-secret")
}
