// Package otel provides OpenTelemetry tracing and metrics support for pg_timetable.
// The Provider is opt-in: when OTel is not configured it returns noop implementations
// with no performance overhead.
package otel

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	nooptrace "go.opentelemetry.io/otel/trace/noop"

	"github.com/cybertec-postgresql/pg_timetable/internal/config"
)

// Provider holds OTel TracerProvider and MeterProvider instances.
// Both are nil when the corresponding signal is not configured, in which case
// Tracer() and Meter() return noop implementations.
type Provider struct {
	tp              *sdktrace.TracerProvider
	mp              *sdkmetric.MeterProvider
	shutdownTimeout time.Duration

	// metric instruments (nil when metrics disabled)
	chainStarted   metric.Int64Counter
	chainCompleted metric.Int64Counter
	chainFailed    metric.Int64Counter
	chainDuration  metric.Float64Histogram
	taskExecuted   metric.Int64Counter
}

// New initialises an OTel Provider from the given options.
// When opts.Endpoint is empty or both Traces and Metrics are false, a noop
// Provider is returned without connecting to any backend.
func New(ctx context.Context, opts config.OTelOpts, clientName, serviceVersion string) (*Provider, error) {
	p := &Provider{
		shutdownTimeout: time.Duration(opts.ShutdownTimeout) * time.Second,
	}
	if opts.Endpoint == "" || (!opts.Traces && !opts.Metrics) {
		return p, nil
	}

	res, err := buildResource(ctx, opts.ServiceName, clientName, serviceVersion)
	if err != nil {
		return p, fmt.Errorf("otel: build resource: %w", err)
	}

	u, err := url.Parse(opts.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("otel: parse endpoint: %w", err)
	}

	if opts.Traces {
		tp, err := buildTracerProvider(ctx, u, opts, res)
		if err != nil {
			return nil, err
		}
		p.tp = tp
	}

	if opts.Metrics {
		mp, err := buildMeterProvider(ctx, u, opts, res)
		if err != nil {
			return nil, err
		}
		p.mp = mp
		if err := p.registerMetrics(); err != nil {
			return nil, err
		}
	}

	return p, nil
}

// NewNoop returns a Provider that produces no spans or metrics and makes no
// outbound connections. Use in tests and as a safe default before full wiring.
func NewNoop() *Provider {
	return &Provider{shutdownTimeout: 5 * time.Second}
}

// Tracer returns the OTel Tracer for pg_timetable instrumentation.
// Returns a noop Tracer when tracing is not configured.
func (p *Provider) Tracer() trace.Tracer {
	if p.tp != nil {
		return p.tp.Tracer("pg_timetable")
	}
	return nooptrace.NewTracerProvider().Tracer("")
}

// Meter returns the OTel Meter for pg_timetable instrumentation.
// Returns a noop Meter when metrics are not configured.
func (p *Provider) Meter() metric.Meter {
	if p.mp != nil {
		return p.mp.Meter("pg_timetable")
	}
	return noop.NewMeterProvider().Meter("")
}

// Shutdown flushes pending spans and metrics and releases resources.
// It is safe to call multiple times.
func (p *Provider) Shutdown(ctx context.Context) error {
	var errs []error
	if p.tp != nil {
		if err := p.tp.Shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if p.mp != nil {
		if err := p.mp.Shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("otel shutdown: %v", errs)
	}
	return nil
}

// ShutdownTimeout returns the configured shutdown flush timeout.
func (p *Provider) ShutdownTimeout() time.Duration {
	return p.shutdownTimeout
}

func buildResource(ctx context.Context, serviceName, clientName, serviceVersion string) (*resource.Resource, error) {
	return resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(serviceVersion),
			attribute.String("client.name", clientName),
		),
	)
}

func buildTracerProvider(ctx context.Context, u *url.URL, opts config.OTelOpts, res *resource.Resource) (*sdktrace.TracerProvider, error) {
	exp, err := buildTraceExporter(ctx, u, opts)
	if err != nil {
		return nil, err
	}
	sampler := sdktrace.ParentBased(sdktrace.TraceIDRatioBased(opts.SampleRatio))
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithSampler(sampler),
		sdktrace.WithResource(res),
	)
	return tp, nil
}

func buildTraceExporter(ctx context.Context, u *url.URL, opts config.OTelOpts) (sdktrace.SpanExporter, error) {
	switch u.Scheme {
	case "grpc":
		grpcOpts := []otlptracegrpc.Option{
			otlptracegrpc.WithEndpoint(u.Host),
			otlptracegrpc.WithHeaders(opts.Headers),
		}
		if opts.Insecure {
			grpcOpts = append(grpcOpts, otlptracegrpc.WithInsecure())
		}
		return otlptracegrpc.New(ctx, grpcOpts...)
	case "http", "https":
		httpOpts := []otlptracehttp.Option{
			otlptracehttp.WithEndpoint(u.Host),
			otlptracehttp.WithHeaders(opts.Headers),
		}
		if opts.Insecure {
			httpOpts = append(httpOpts, otlptracehttp.WithInsecure())
		}
		return otlptracehttp.New(ctx, httpOpts...)
	default:
		return nil, fmt.Errorf("unsupported OTel endpoint scheme: %s", u.Scheme)
	}
}

func buildMeterProvider(ctx context.Context, u *url.URL, opts config.OTelOpts, res *resource.Resource) (*sdkmetric.MeterProvider, error) {
	exp, err := buildMetricExporter(ctx, u, opts)
	if err != nil {
		return nil, err
	}
	reader := sdkmetric.NewPeriodicReader(exp,
		sdkmetric.WithInterval(time.Duration(opts.MetricPeriod)*time.Second),
	)
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(reader),
		sdkmetric.WithResource(res),
	)
	return mp, nil
}

func buildMetricExporter(ctx context.Context, u *url.URL, opts config.OTelOpts) (sdkmetric.Exporter, error) {
	switch u.Scheme {
	case "grpc":
		grpcOpts := []otlpmetricgrpc.Option{
			otlpmetricgrpc.WithEndpoint(u.Host),
			otlpmetricgrpc.WithHeaders(opts.Headers),
		}
		if opts.Insecure {
			grpcOpts = append(grpcOpts, otlpmetricgrpc.WithInsecure())
		}
		return otlpmetricgrpc.New(ctx, grpcOpts...)
	case "http", "https":
		httpOpts := []otlpmetrichttp.Option{
			otlpmetrichttp.WithEndpoint(u.Host),
			otlpmetrichttp.WithHeaders(opts.Headers),
		}
		if opts.Insecure {
			httpOpts = append(httpOpts, otlpmetrichttp.WithInsecure())
		}
		return otlpmetrichttp.New(ctx, httpOpts...)
	default:
		return nil, fmt.Errorf("unsupported OTel endpoint scheme: %s", u.Scheme)
	}
}
