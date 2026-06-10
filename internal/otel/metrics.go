package otel

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var chainDurationBuckets = []float64{0.001, 0.01, 0.1, 0.5, 1, 5, 10, 30, 60, 120, 300}

func (p *Provider) registerMetrics() error {
	m := p.Meter()
	var err error

	p.chainStarted, err = m.Int64Counter("pgtimetable.chain.started",
		metric.WithDescription("Number of chain executions started"),
		metric.WithUnit("{execution}"))
	if err != nil {
		return err
	}

	p.chainCompleted, err = m.Int64Counter("pgtimetable.chain.completed",
		metric.WithDescription("Number of chain executions completed successfully"),
		metric.WithUnit("{execution}"))
	if err != nil {
		return err
	}

	p.chainFailed, err = m.Int64Counter("pgtimetable.chain.failed",
		metric.WithDescription("Number of chain executions that failed"),
		metric.WithUnit("{execution}"))
	if err != nil {
		return err
	}

	p.chainDuration, err = m.Float64Histogram("pgtimetable.chain.duration",
		metric.WithDescription("Chain execution duration"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(chainDurationBuckets...))
	if err != nil {
		return err
	}

	p.taskExecuted, err = m.Int64Counter("pgtimetable.task.executed",
		metric.WithDescription("Number of tasks executed"),
		metric.WithUnit("{execution}"))
	if err != nil {
		return err
	}

	return nil
}

// RecordChainStarted increments the chain started counter.
func (p *Provider) RecordChainStarted(ctx context.Context, clientName string) {
	if p.chainStarted != nil {
		p.chainStarted.Add(ctx, 1, metric.WithAttributes(attribute.String("client.name", clientName)))
	}
}

// RecordChainCompleted increments the chain completed counter.
func (p *Provider) RecordChainCompleted(ctx context.Context, clientName string) {
	if p.chainCompleted != nil {
		p.chainCompleted.Add(ctx, 1, metric.WithAttributes(attribute.String("client.name", clientName)))
	}
}

// RecordChainFailed increments the chain failed counter.
func (p *Provider) RecordChainFailed(ctx context.Context, clientName string) {
	if p.chainFailed != nil {
		p.chainFailed.Add(ctx, 1, metric.WithAttributes(attribute.String("client.name", clientName)))
	}
}

// RecordChainDuration records the chain execution duration in seconds.
func (p *Provider) RecordChainDuration(ctx context.Context, seconds float64, clientName string) {
	if p.chainDuration != nil {
		p.chainDuration.Record(ctx, seconds, metric.WithAttributes(attribute.String("client.name", clientName)))
	}
}

// RecordTaskExecuted increments the task executed counter.
func (p *Provider) RecordTaskExecuted(ctx context.Context, clientName, taskKind string) {
	if p.taskExecuted != nil {
		p.taskExecuted.Add(ctx, 1, metric.WithAttributes(
			attribute.String("client.name", clientName),
			attribute.String("task.kind", taskKind),
		))
	}
}
