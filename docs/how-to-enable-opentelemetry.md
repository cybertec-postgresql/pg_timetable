# Enable OpenTelemetry

!!! note

    OTel support is fully **opt-in**. When `--otel-endpoint` is not configured, pg_timetable
    behaves exactly as before with zero additional overhead.

## Signals

pg_timetable supports two OTel signals, each independently enabled:

| Signal | Flag | What it provides |
|--------|------|-----------------|
| **Traces** | `--otel-traces` | A distributed trace per chain execution with child spans for every task |
| **Metrics** | `--otel-metrics` | Counters and a histogram covering chain and task throughput |

## Quick Start

### All signals at once (recommended)

The easiest way to get both traces and metrics working locally is the
[`grafana/otel-lgtm`](https://github.com/grafana/otel-lgtm) image. It bundles an OTel Collector,
Grafana, Tempo (traces), and Mimir (metrics) in a single container — no configuration required.

```bash
# 1. Start the all-in-one observability stack
docker run --rm -d \
  -p 4317:4317 \
  -p 4318:4318 \
  -p 3000:3000 \
  grafana/otel-lgtm

# 2. Run pg_timetable with both signals enabled
pg_timetable \
  --otel-endpoint grpc://localhost:4317 \
  --otel-traces \
  --otel-metrics \
  --otel-insecure \
  postgresql://scheduler:pass@localhost/mydb
```

Open <http://localhost:3000> (default credentials `admin`/`admin`) to explore traces in **Tempo**
and metrics in **Mimir** via Grafana.

### Traces only — Jaeger

!!! warning "Traces only"
    Jaeger implements the OTLP **trace** service only. Enabling `--otel-metrics` with a Jaeger
    endpoint will produce export errors. Use this setup when you need traces exclusively.

```bash
# 1. Start Jaeger
docker run --rm -d -p 4317:4317 -p 16686:16686 jaegertracing/all-in-one:latest

# 2. Run pg_timetable with tracing only
pg_timetable \
  --otel-endpoint grpc://localhost:4317 \
  --otel-traces \
  --otel-insecure \
  postgresql://scheduler:pass@localhost/mydb
```

Open <http://localhost:16686> and select service **pg_timetable** to see traces.

### Metrics only — OTel Collector → Prometheus

```bash
pg_timetable \
  --otel-endpoint grpc://otel-collector:4317 \
  --otel-metrics \
  --otel-metric-period 15 \
  postgresql://scheduler:pass@localhost/mydb
```

## Reduce Trace Volume

By default, 100 % of chain executions are traced. For high-frequency deployments you can
reduce trace volume with a ratio sampler:

```bash
# Trace 10 % of chain executions
pg_timetable --otel-traces --otel-sample-ratio 0.1 \
  --otel-endpoint grpc://localhost:4317 \
  postgresql://scheduler:pass@localhost/mydb
```

For every flag, span attribute, metric, and config key, see the [OpenTelemetry Reference](reference-opentelemetry.md).
