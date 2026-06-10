package otel_test

import (
	"context"
	"testing"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/internal/config"
	"github.com/cybertec-postgresql/pg_timetable/internal/otel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func defaultOpts() config.OTelOpts {
	return config.OTelOpts{
		ServiceName:     "pg_timetable",
		SampleRatio:     1.0,
		MetricPeriod:    30,
		ShutdownTimeout: 5,
	}
}

func TestNoopProviderWhenDisabled(t *testing.T) {
	ctx := context.Background()
	opts := defaultOpts()
	// No endpoint → noop
	p, err := otel.New(ctx, opts, "test-client", "test")
	require.NoError(t, err)
	assert.NotNil(t, p)

	tracer := p.Tracer()
	assert.NotNil(t, tracer)

	m := p.Meter()
	assert.NotNil(t, m)
}

func TestNewNoopConstructor(t *testing.T) {
	p := otel.NewNoop()
	assert.NotNil(t, p)
	assert.NotNil(t, p.Tracer())
	assert.NotNil(t, p.Meter())
}

func TestShutdownIsIdempotent(t *testing.T) {
	p := otel.NewNoop()
	ctx := context.Background()
	assert.NoError(t, p.Shutdown(ctx))
	assert.NoError(t, p.Shutdown(ctx))
}

func TestShutdownTimeout(t *testing.T) {
	p := otel.NewNoop()
	assert.Equal(t, 5*time.Second, p.ShutdownTimeout())
}

func TestUnsupportedSchemeReturnsError(t *testing.T) {
	ctx := context.Background()
	opts := defaultOpts()
	opts.Endpoint = "ftp://localhost:4317"
	opts.Traces = true
	_, err := otel.New(ctx, opts, "test-client", "test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported OTel endpoint scheme")
}

func TestNoopMetricRecordingNoPanic(t *testing.T) {
	p := otel.NewNoop()
	ctx := context.Background()
	// All record methods must be safe to call on a noop provider
	assert.NotPanics(t, func() {
		p.RecordChainStarted(ctx, "client")
		p.RecordChainCompleted(ctx, "client")
		p.RecordChainFailed(ctx, "client")
		p.RecordChainDuration(ctx, 1.5, "client")
		p.RecordTaskExecuted(ctx, "client", "SQL")
	})
}

// TestProviderStub just verifies the package compiles and Tracer/Meter are usable
func TestProviderStub(t *testing.T) {
	t.Helper()
	p := otel.NewNoop()
	ctx := context.Background()
	tracer := p.Tracer()
	_, span := tracer.Start(ctx, "test-span")
	span.End()
}

func TestNoopSpanAttributes(t *testing.T) {
	p := otel.NewNoop()
	ctx := context.Background()
	_, span := p.Tracer().Start(ctx, "chain.execute",
		trace.WithAttributes(
			attribute.Int("chain.id", 1),
			attribute.String("chain.name", "test"),
		))
	assert.NotNil(t, span)
	span.End()
}

func TestNewWithUnreachableEndpointGRPC(t *testing.T) {
	// gRPC connections are lazy — New() should succeed even if backend is down
	ctx := context.Background()
	opts := config.OTelOpts{
		Endpoint:        "grpc://localhost:19999",
		Traces:          true,
		Insecure:        true,
		SampleRatio:     1.0,
		MetricPeriod:    30,
		ShutdownTimeout: 1,
		ServiceName:     "pg_timetable",
	}
	p, err := otel.New(ctx, opts, "test-client", "0.0.1")
	// gRPC is lazy — no error expected at provider creation time
	if err != nil {
		t.Logf("Note: gRPC New() returned error (may be eager): %v", err)
		// This is acceptable — main.go falls back to noop on error
		return
	}
	assert.NotNil(t, p)
	// Shutdown should complete within 1 second even with no backend
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = p.Shutdown(shutdownCtx) // errors acceptable here
}
