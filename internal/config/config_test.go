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
