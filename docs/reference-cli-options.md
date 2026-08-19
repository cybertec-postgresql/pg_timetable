# Command-Line Reference

Complete list of pg_timetable command-line flags and their environment-variable equivalents.

```bash
# ./pg_timetable

Application Options:
  -c, --clientname=                                Unique name for application instance [$PGTT_CLIENTNAME]
      --config=                                    YAML configuration file
      --no-program-tasks                           Disable executing of PROGRAM tasks [$PGTT_NOPROGRAMTASKS]
  -v, --version                                    Output detailed version information [$PGTT_VERSION]
      --connstr                                    PostgreSQL connection string [$PGTT_CONNSTR]

Logging:
      --log-level=[debug|info|error]               Verbosity level for stdout and log file (default: info)
      --log-database-level=[debug|info|error|none] Verbosity level for database storing (default: info)
      --log-file=                                  File name to store logs
      --log-file-format=[json|text]                Format of file logs (default: json)
      --log-file-rotate                            Rotate log files
      --log-file-size=                             Maximum size in MB of the log file before it gets rotated (default: 100)
      --log-file-age=                              Number of days to retain old log files, 0 means forever (default: 0)
      --log-file-number=                           Maximum number of old log files to retain, 0 to retain all (default: 0)

Start:
  -f, --file=                                      SQL script or YAML chain definition file to execute during
                                                   startup; may be specified multiple times
      --replace                                    Replace existing chains when loading YAML files
      --validate                                   Only validate YAML file without importing chains
      --init                                       Initialize database schema to the latest version and exit. Can be used
                                                   with --upgrade
      --upgrade                                    Upgrade database to the latest version
      --debug                                      Run in debug mode. Only asynchronous chains will be executed

Resource:
      --cron-workers=                              Number of parallel workers for scheduled chains (default: 16)
      --interval-workers=                          Number of parallel workers for interval chains (default: 16)
      --chain-timeout=                             Abort any chain that takes more than the specified number of
                                                   milliseconds
      --task-timeout=                              Abort any task within a chain that takes more than the specified number
                                                   of milliseconds

REST:
      --rest-port=                                 REST API port (default: 0) [$PGTT_RESTPORT]

OTel:
      --otel-endpoint=                             OTLP exporter endpoint URL (grpc://, http://, https://)
      --otel-traces                                Enable OpenTelemetry distributed tracing
      --otel-metrics                               Enable OpenTelemetry metrics export
      --otel-service-name=                         OTel service.name resource attribute (default: pg_timetable)
      --otel-insecure                              Disable TLS for OTLP connection (dev/test only)
      --otel-sample-ratio=                         Trace sampling ratio 0.0-1.0 (default: 1.0)
      --otel-metric-period=                        Metrics export interval in seconds (default: 30)
      --otel-shutdown-timeout=                     OTel provider flush timeout in seconds on shutdown (default: 5)
```
