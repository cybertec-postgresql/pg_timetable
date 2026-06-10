package otel

import (
	"context"
	"net/url"
	"testing"

	"github.com/cybertec-postgresql/pg_timetable/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

func testOTelOpts(endpoint string) config.OTelOpts {
	return config.OTelOpts{
		Endpoint:        endpoint,
		Traces:          true,
		Metrics:         true,
		ServiceName:     "pg_timetable",
		Headers:         map[string]string{"x-test-header": "value"},
		Insecure:        true,
		SampleRatio:     0.5,
		MetricPeriod:    1,
		ShutdownTimeout: 2,
	}
}

func TestBuildResourceIncludesServiceAttributes(t *testing.T) {
	res, err := buildResource(context.Background(), "pg_timetable", "worker-a", "1.2.3")
	require.NoError(t, err)

	serviceName, ok := res.Set().Value(semconv.ServiceNameKey)
	require.True(t, ok)
	assert.Equal(t, "pg_timetable", serviceName.AsString())

	serviceVersion, ok := res.Set().Value(semconv.ServiceVersionKey)
	require.True(t, ok)
	assert.Equal(t, "1.2.3", serviceVersion.AsString())

	clientName, ok := res.Set().Value(attribute.Key("client.name"))
	require.True(t, ok)
	assert.Equal(t, "worker-a", clientName.AsString())
}

func TestBuildExportersSupportGRPCAndHTTP(t *testing.T) {
	testCases := []struct {
		name     string
		endpoint string
		insecure bool
	}{
		{name: "grpc", endpoint: "grpc://localhost:4317", insecure: true},
		{name: "http", endpoint: "http://localhost:4318", insecure: true},
		{name: "https", endpoint: "https://localhost:4318", insecure: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			u, err := url.Parse(tc.endpoint)
			require.NoError(t, err)

			opts := testOTelOpts(tc.endpoint)
			opts.Insecure = tc.insecure

			traceExporter, err := buildTraceExporter(context.Background(), u, opts)
			require.NoError(t, err)
			assert.NotNil(t, traceExporter)

			metricExporter, err := buildMetricExporter(context.Background(), u, opts)
			require.NoError(t, err)
			assert.NotNil(t, metricExporter)
		})
	}
}

func TestNewConfiguresProvidersAndMetrics(t *testing.T) {
	p, err := New(context.Background(), testOTelOpts("grpc://localhost:4317"), "worker-a", "1.2.3")
	require.NoError(t, err)
	require.NotNil(t, p)

	assert.NotNil(t, p.tp)
	assert.NotNil(t, p.mp)
	assert.NotNil(t, p.chainStarted)
	assert.NotNil(t, p.chainCompleted)
	assert.NotNil(t, p.chainFailed)
	assert.NotNil(t, p.chainDuration)
	assert.NotNil(t, p.taskExecuted)

	_, span := p.Tracer().Start(context.Background(), "configured-span")
	span.End()

	_, err = p.Meter().Int64Counter("configured-counter")
	require.NoError(t, err)
	assert.Equal(t, 2_000_000_000, int(p.ShutdownTimeout()))

	shutdownCtx, cancel := context.WithTimeout(context.Background(), p.ShutdownTimeout())
	defer cancel()
	_ = p.Shutdown(shutdownCtx)
	_ = p.Shutdown(shutdownCtx)
}

func TestNewReturnsEndpointParseError(t *testing.T) {
	_, err := New(context.Background(), testOTelOpts("http://[::1"), "worker-a", "1.2.3")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "otel: parse endpoint")
}

func TestRegisterMetricsRecordsMeasurements(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(reader),
		sdkmetric.WithResource(resource.Empty()),
	)
	p := &Provider{mp: mp}

	require.NoError(t, p.registerMetrics())

	ctx := context.Background()
	p.RecordChainStarted(ctx, "worker-a")
	p.RecordChainCompleted(ctx, "worker-a")
	p.RecordChainFailed(ctx, "worker-a")
	p.RecordChainDuration(ctx, 3.5, "worker-a")
	p.RecordTaskExecuted(ctx, "worker-a", "SQL")
	p.RecordTaskExecuted(ctx, "worker-a", "PROGRAM")

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &rm))
	require.Len(t, rm.ScopeMetrics, 1)
	require.Len(t, rm.ScopeMetrics[0].Metrics, 5)

	metricsByName := make(map[string]metricdata.Metrics, len(rm.ScopeMetrics[0].Metrics))
	for _, metric := range rm.ScopeMetrics[0].Metrics {
		metricsByName[metric.Name] = metric
	}

	started := metricsByName["pgtimetable.chain.started"].Data.(metricdata.Sum[int64])
	require.Len(t, started.DataPoints, 1)
	assert.Equal(t, int64(1), started.DataPoints[0].Value)

	completed := metricsByName["pgtimetable.chain.completed"].Data.(metricdata.Sum[int64])
	require.Len(t, completed.DataPoints, 1)
	assert.Equal(t, int64(1), completed.DataPoints[0].Value)

	failed := metricsByName["pgtimetable.chain.failed"].Data.(metricdata.Sum[int64])
	require.Len(t, failed.DataPoints, 1)
	assert.Equal(t, int64(1), failed.DataPoints[0].Value)

	duration := metricsByName["pgtimetable.chain.duration"].Data.(metricdata.Histogram[float64])
	require.Len(t, duration.DataPoints, 1)
	assert.Equal(t, uint64(1), duration.DataPoints[0].Count)

	tasks := metricsByName["pgtimetable.task.executed"].Data.(metricdata.Sum[int64])
	require.Len(t, tasks.DataPoints, 2)
	assert.ElementsMatch(t, []int64{1, 1}, []int64{tasks.DataPoints[0].Value, tasks.DataPoints[1].Value})
	assert.Equal(t, int64(2), tasks.DataPoints[0].Value+tasks.DataPoints[1].Value)

	require.NoError(t, mp.Shutdown(ctx))
	assert.NoError(t, (&Provider{tp: sdktrace.NewTracerProvider(), mp: sdkmetric.NewMeterProvider()}).Shutdown(ctx))
}