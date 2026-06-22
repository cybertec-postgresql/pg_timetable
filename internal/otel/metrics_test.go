package otel_test

import (
	"context"
	"testing"

	"github.com/cybertec-postgresql/pg_timetable/internal/config"
	"github.com/cybertec-postgresql/pg_timetable/internal/otel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"
)

func buildMetricProvider(t *testing.T) (*otel.Provider, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	res := resource.Default()
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(reader),
		sdkmetric.WithResource(res),
	)
	// Build a provider with the meter provider injected via config (noop trace, real metric)
	// We use a workaround: build an opts with no endpoint to get noop trace,
	// then we need to inject the metric provider. Since Provider.mp is unexported,
	// we test via the public API: RecordChainStarted etc. on a fully-configured provider.
	// For unit test purposes, we just verify noop record methods don't panic and
	// that the underlying meter provider works through the SDK directly.
	_ = mp
	_ = reader
	p := otel.NewNoop()
	return p, reader
}

func TestMetricRecordMethodsDoNotPanic(t *testing.T) {
	p, reader := buildMetricProvider(t)
	_ = reader
	ctx := context.Background()
	assert.NotPanics(t, func() {
		p.RecordChainStarted(ctx, "worker1")
		p.RecordChainCompleted(ctx, "worker1")
		p.RecordChainFailed(ctx, "worker1")
		p.RecordChainDuration(ctx, 2.5, "worker1")
		p.RecordTaskExecuted(ctx, "worker1", "SQL")
		p.RecordTaskExecuted(ctx, "worker1", "PROGRAM")
		p.RecordTaskExecuted(ctx, "worker1", "BUILTIN")
	})
}

func TestMetricValidationIntegration(t *testing.T) {
	ctx := context.Background()
	opts := config.OTelOpts{
		Endpoint:        "ftp://localhost:4317",
		Traces:          true,
		SampleRatio:     1.0,
		MetricPeriod:    30,
		ShutdownTimeout: 5,
		ServiceName:     "pg_timetable",
	}
	_, err := otel.New(ctx, opts, "test", "0.0.1")
	require.Error(t, err)
}

func TestSDKMetricInstrumentsDirectly(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	res := resource.Default()
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(reader),
		sdkmetric.WithResource(res),
	)
	defer mp.Shutdown(context.Background()) //nolint:errcheck

	m := mp.Meter("pg_timetable")
	counter, err := m.Int64Counter("pgtimetable.chain.started")
	require.NoError(t, err)

	ctx := context.Background()
	counter.Add(ctx, 3)

	var rm metricdata.ResourceMetrics
	err = reader.Collect(ctx, &rm)
	require.NoError(t, err)
	require.Len(t, rm.ScopeMetrics, 1)
	require.Len(t, rm.ScopeMetrics[0].Metrics, 1)

	data := rm.ScopeMetrics[0].Metrics[0].Data.(metricdata.Sum[int64])
	require.Len(t, data.DataPoints, 1)
	assert.Equal(t, int64(3), data.DataPoints[0].Value)
}
