package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfig(t *testing.T) {
	os.Args = []string{0: "config_test", "--config=../../config.example.yaml"}
	_, err := NewConfig(nil)
	assert.NoError(t, err)

	os.Args = []string{0: "config_test", "--unknown"}
	_, err = NewConfig(nil)
	assert.Error(t, err)

	os.Args = []string{0: "config_test"} // clientname arg is missing
	_, err = NewConfig(nil)
	assert.Error(t, err)

	os.Args = []string{0: "config_test", "--config=foo.boo.bar.baz.yaml"}
	_, err = NewConfig(nil)
	assert.Error(t, err)

	os.Args = []string{0: "config_test"} // clientname arg is missing, but set PGTT_CLIENTNAME
	assert.NoError(t, os.Setenv("PGTT_CLIENTNAME", "worker001"))
	_, err = NewConfig(nil)
	assert.NoError(t, err)
	assert.NoError(t, os.Unsetenv("PGTT_CLIENTNAME"))
}

// TestConfigServiceSkipsDaemonRequirements covers the service-management early
// return in NewConfig: when --service is given, the config is returned without
// requiring --clientname or validating OTel, because those operations neither
// connect to the database nor export telemetry.
func TestConfigServiceSkipsDaemonRequirements(t *testing.T) {
	for _, action := range []string{"install", "uninstall", "start", "stop", "restart", "status"} {
		t.Run(action, func(t *testing.T) {
			// No --clientname and a deliberately invalid OTel sample ratio:
			// both would fail for a normal daemon start but must be skipped
			// for a --service operation.
			os.Args = []string{0: "config_test", "--service=" + action, "--otel-sample-ratio=42"}
			conf, err := NewConfig(nil)
			assert.NoError(t, err, "service operations must skip daemon-only validation")
			assert.Equal(t, action, conf.Service.Service)
			assert.Empty(t, conf.ClientName)
		})
	}
}

func TestConfigFileFlag(t *testing.T) {
	// No --file should result in an empty slice, not [""] or ["[]"]
	os.Args = []string{0: "config_test", "--clientname=worker"}
	conf, err := NewConfig(nil)
	assert.NoError(t, err)
	assert.Empty(t, conf.Start.File)

	// A single --file must round-trip without bracket/quote artifacts
	os.Args = []string{0: "config_test", "--clientname=worker", "--file=../../samples/Basic.sql"}
	conf, err = NewConfig(nil)
	assert.NoError(t, err)
	assert.Equal(t, []string{"../../samples/Basic.sql"}, conf.Start.File)

	// Multiple --file values must all be preserved
	os.Args = []string{0: "config_test", "--clientname=worker",
		"--file=../../samples/Basic.sql", "--file=../../samples/Chain.sql"}
	conf, err = NewConfig(nil)
	assert.NoError(t, err)
	assert.Equal(t, []string{"../../samples/Basic.sql", "../../samples/Chain.sql"}, conf.Start.File)
}

func TestValidateOTel(t *testing.T) {
	tests := []struct {
		name    string
		opts    OTelOpts
		wantErr string
	}{
		{
			name: "valid default config",
			opts: OTelOpts{SampleRatio: 1.0, MetricPeriod: 30, ShutdownTimeout: 5},
		},
		{
			name:    "sample ratio too high",
			opts:    OTelOpts{SampleRatio: 1.5, MetricPeriod: 30, ShutdownTimeout: 5},
			wantErr: "otel-sample-ratio must be between 0.0 and 1.0",
		},
		{
			name:    "sample ratio negative",
			opts:    OTelOpts{SampleRatio: -0.1, MetricPeriod: 30, ShutdownTimeout: 5},
			wantErr: "otel-sample-ratio must be between 0.0 and 1.0",
		},
		{
			name:    "metric period zero",
			opts:    OTelOpts{SampleRatio: 1.0, MetricPeriod: 0, ShutdownTimeout: 5},
			wantErr: "otel-metric-period must be > 0",
		},
		{
			name:    "shutdown timeout zero",
			opts:    OTelOpts{SampleRatio: 1.0, MetricPeriod: 30, ShutdownTimeout: 0},
			wantErr: "otel-shutdown-timeout must be > 0",
		},
		{
			name:    "unsupported endpoint scheme",
			opts:    OTelOpts{SampleRatio: 1.0, MetricPeriod: 30, ShutdownTimeout: 5, Endpoint: "ftp://localhost:4317"},
			wantErr: "unsupported OTel endpoint scheme: ftp",
		},
		{
			name: "valid grpc endpoint",
			opts: OTelOpts{SampleRatio: 1.0, MetricPeriod: 30, ShutdownTimeout: 5, Endpoint: "grpc://localhost:4317"},
		},
		{
			name: "valid https endpoint",
			opts: OTelOpts{SampleRatio: 1.0, MetricPeriod: 30, ShutdownTimeout: 5, Endpoint: "https://api.honeycomb.io"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateOTel(tt.opts)
			if tt.wantErr == "" {
				assert.NoError(t, err)
			} else {
				assert.EqualError(t, err, tt.wantErr)
			}
		})
	}
}

func TestSecretKeyConfigBinding(t *testing.T) {
	// NewConfig MUST bind --secret-key and PGTT_SECRET_KEY
	// to ConfigOptions.SecretEncryptionKey. This guards the mandatory
	// mapstructure tag.
	const want = "the-test-secret-key"

	os.Args = []string{"config_test", "--clientname=worker", "--secret-key=" + want}
	conf, err := NewConfig(nil)
	assert.NoError(t, err)
	assert.Equal(t, want, conf.SecretEncryptionKey)

	assert.NoError(t, os.Setenv("PGTT_SECRET_KEY", want))
	defer os.Unsetenv("PGTT_SECRET_KEY")
	os.Args = []string{"config_test", "--clientname=worker"}
	conf, err = NewConfig(nil)
	assert.NoError(t, err)
	assert.Equal(t, want, conf.SecretEncryptionKey)
}
