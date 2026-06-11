# OpenTelemetry Support

**pg_timetable** has built-in support for [OpenTelemetry](https://opentelemetry.io/) (OTel), the
industry-standard observability framework. When enabled, pg_timetable exports distributed traces
and metrics to any OTLP-compatible backend (Jaeger, Grafana Tempo, Honeycomb, Datadog, etc.)
without any code changes — purely through configuration.

!!! note

    OTel support is fully **opt-in**. When `--otel-endpoint` is not configured, pg_timetable
    behaves exactly as before with zero additional overhead.

---

## Signals

pg_timetable supports two OTel signals, each independently enabled:

| Signal | Flag | What it provides |
|--------|------|-----------------|
| **Traces** | `--otel-traces` | A distributed trace per chain execution with child spans for every task |
| **Metrics** | `--otel-metrics` | Counters and a histogram covering chain and task throughput |

---

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

---

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

---

## Protocol Selection

The OTLP transport protocol is inferred automatically from the endpoint URL scheme:

| Scheme | Transport |
|--------|-----------|
| `grpc://` | OTLP/gRPC |
| `http://` | OTLP/HTTP (protobuf) |
| `https://` | OTLP/HTTP with TLS |

TLS is **enabled by default** for all transports. Use `--otel-insecure` to disable TLS verification
in development environments.

---

## Trace Schema

Each chain execution produces a root span **`chain.execute`** containing child spans
**`task.execute`** for every task in the chain.

### Span: `chain.execute`

| Attribute | Value |
|-----------|-------|
| `chain.id` | Chain ID (integer) |
| `chain.name` | Chain name |
| `client.name` | pg_timetable client name (`--clientname`) |

### Span: `task.execute`

| Attribute | Value |
|-----------|-------|
| `task.name` | Task command |
| `task.kind` | `SQL`, `PROGRAM`, or `BUILTIN` |
| `task.return_code` | `0` on success, `-1` on failure |

Failed tasks produce an OTel **error event** with the error message, allowing trace-based
alerting and root-cause analysis.

---

## Metric Instruments

All instruments are registered under the meter name `pg_timetable` and carry the
`client.name` attribute for multi-instance deployments.

| Instrument | Kind | Unit | Description |
|-----------|------|------|-------------|
| `pgtimetable.chain.started` | Counter | `{execution}` | Chain executions started |
| `pgtimetable.chain.completed` | Counter | `{execution}` | Chain executions completed successfully |
| `pgtimetable.chain.failed` | Counter | `{execution}` | Chain executions that failed |
| `pgtimetable.chain.duration` | Histogram | `s` | Wall-clock duration of chain execution |
| `pgtimetable.task.executed` | Counter | `{execution}` | Tasks executed (labelled by `task.kind`) |

The histogram uses these explicit bucket boundaries (seconds):
`0.001, 0.01, 0.1, 0.5, 1, 5, 10, 30, 60, 120, 300`

---

## Authentication & Security

SaaS observability backends (Honeycomb, Grafana Cloud, Datadog, etc.) typically require an
API key. Pass it as a custom HTTP header via the YAML configuration file:

```yaml
otel:
  endpoint: https://api.honeycomb.io
  traces: true
  headers:
    x-honeycomb-team: YOUR_API_KEY
```

!!! warning

    `otel.headers` can only be set via the YAML configuration file — it is not available
    as a CLI flag to prevent API keys from appearing in shell history or process listings.
    Header values are **never written to log output**.

---

## Sampling

By default, 100 % of chain executions are traced. For high-frequency deployments you can
reduce trace volume with a ratio sampler:

```bash
# Trace 10 % of chain executions
pg_timetable --otel-traces --otel-sample-ratio 0.1 \
  --otel-endpoint grpc://localhost:4317 \
  postgresql://scheduler:pass@localhost/mydb
```

| Value | Effect |
|-------|--------|
| `1.0` (default) | Every chain execution is traced |
| `0.5` | ~50 % of executions traced |
| `0.0` | No traces generated |

---

## Resilience

- **Unreachable backend**: pg_timetable starts normally and logs a `WARN` message. Chain
  scheduling is never interrupted by OTel export failures.
- **Graceful shutdown**: On `SIGTERM`, pg_timetable flushes pending spans and metrics before
  exiting. The flush timeout is controlled by `--otel-shutdown-timeout` (default: 5 s).

---

## CLI Reference

All OTel flags are optional. When `--otel-endpoint` is absent, all other OTel flags are ignored.

```text
OTel:
  --otel-endpoint=          OTLP exporter endpoint URL (grpc://, http://, https://)
  --otel-traces             Enable OpenTelemetry distributed tracing
  --otel-metrics            Enable OpenTelemetry metrics export
  --otel-service-name=      OTel service.name resource attribute (default: pg_timetable)
  --otel-insecure           Disable TLS for OTLP connection (dev/test only)
  --otel-sample-ratio=      Trace sampling ratio 0.0–1.0 (default: 1.0)
  --otel-metric-period=     Metrics export interval in seconds (default: 30)
  --otel-shutdown-timeout=  OTel provider flush timeout in seconds on shutdown (default: 5)
```

---

## YAML Configuration

```yaml
# - OpenTelemetry Settings -
otel:
  # OTLP exporter endpoint URL (grpc://, http://, https://)
  endpoint: ""

  # Enable distributed tracing (default: false)
  traces: false

  # Enable metrics export (default: false)
  metrics: false

  # OTel service.name resource attribute (default: pg_timetable)
  service-name: pg_timetable

  # Custom headers for OTLP export — use for API key auth (map of key: value)
  headers: {}

  # Disable TLS for OTLP connection — dev only (default: false)
  insecure: false

  # Trace sampling ratio 0.0–1.0 (default: 1.0 = 100%)
  sample-ratio: 1.0

  # Metrics export interval in seconds (default: 30)
  metric-period: 30

  # OTel flush timeout in seconds on shutdown (default: 5)
  shutdown-timeout: 5
```

---

## OTel Resource Attributes

Every span and metric datapoint carries these resource attributes identifying the
pg_timetable instance:

| Attribute | Value |
|-----------|-------|
| `service.name` | `--otel-service-name` (default: `pg_timetable`) |
| `service.version` | pg_timetable binary version |
| `client.name` | `--clientname` value |
