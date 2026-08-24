# OpenTelemetry Reference

OTel command-line flags are documented in the [Command-Line Reference](reference-cli-options.md).

## Protocol Selection

The OTLP transport protocol is inferred automatically from the endpoint URL scheme:

| Scheme | Transport |
|--------|-----------|
| `grpc://` | OTLP/gRPC |
| `http://` | OTLP/HTTP (protobuf) |
| `https://` | OTLP/HTTP with TLS |

TLS is **enabled by default** for all transports. Use `--otel-insecure` to disable TLS verification
in development environments.

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

## Sampling Ratio

| Value | Effect |
|-------|--------|
| `1.0` (default) | Every chain execution is traced |
| `0.5` | ~50 % of executions traced |
| `0.0` | No traces generated |

## Resilience

- **Unreachable backend**: pg_timetable starts normally and logs a `WARN` message. Chain
  scheduling is never interrupted by OTel export failures.
- **Graceful shutdown**: On `SIGTERM`, pg_timetable flushes pending spans and metrics before
  exiting. The flush timeout is controlled by `--otel-shutdown-timeout` (default: 5 s).

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

## OTel Resource Attributes

Every span and metric datapoint carries these resource attributes identifying the
pg_timetable instance:

| Attribute | Value |
|-----------|-------|
| `service.name` | `--otel-service-name` (default: `pg_timetable`) |
| `service.version` | pg_timetable binary version |
| `client.name` | `--clientname` value |
