# Introduction

**pg_timetable** is an advanced job scheduler for PostgreSQL, offering many advantages over traditional schedulers such as **cron** and others.
It is completely database driven and provides a couple of advanced concepts.

## Main features

- Tasks can be arranged in chains
- A chain can consist of built-int commands, SQL and executables
- Parameters can be passed to chains
- Missed tasks (possibly due to downtime) can be retried automatically
- Support for configurable repetitions
- Built-in tasks such as sending emails, etc.
- Fully database driven configuration
- Full support for database driven logging
- Cron-style scheduling at the PostgreSQL server time zone
- Optional concurrency protection
- Chains and individual tasks can be enabled or disabled without deleting them
- YAML-based chain definitions for file-based configuration
- Task and chain can have execution timeout settings
- OpenTelemetry tracing and metrics export (opt-in)

New to pg_timetable? Follow the [tutorial](tutorial-first-chain.md) to schedule your first job in a few minutes.

## Learn More

- **Tutorials**: [Your First Scheduled Chain](tutorial-first-chain.md) ·
  [Your First YAML Chain](tutorial-your-first-yaml-chain.md) ·
  [Handling Chain Errors](tutorial-handling-chain-errors.md) ·
  [Storing a Secret](tutorial-storing-a-secret.md)
- **Chain scheduling**: [Schedule Common Jobs](how-to-schedule-common-jobs.md) ·
  [Commands, Tasks, and Chains Reference](reference-commands-tasks-chains.md)
- **YAML authoring**: [Define Chains in YAML](how-to-write-yaml-chains.md) ·
  [YAML Chain Schema](yaml-format.md)
- **Secrets**: [Use the Secret Store](how-to-use-secret-store.md) ·
  [Secret Store Security Model](explanation-secret-store-security-model.md)
- **Observability**: [Enable OpenTelemetry](how-to-enable-opentelemetry.md) ·
  [OpenTelemetry Reference](reference-opentelemetry.md)
- **Concept**: [The Scheduling Model](explanation-scheduling-model.md) ·
  [Project Background](background.md)

See the sidebar navigation for the complete Reference and How-to Guides sections.

## Contributing

If you want to contribute to **pg_timetable** and help make it better, feel free to open an 
[issue](https://github.com/cybertec-postgresql/pg_timetable/issues) or even consider submitting a 
[pull request](https://github.com/cybertec-postgresql/pg_timetable/pulls). You also can give a 
[star](https://github.com/cybertec-postgresql/pg_timetable/stargazers) to **pg_timetable** project, 
and to tell the world about it.

## Support

For professional support, please contact [Cybertec](https://www.cybertec-postgresql.com/).

## Authors

**Implementation:** [Pavlo Golub](https://github.com/pashagolub) 

**Initial idea and draft design:** [Hans-Jürgen Schönig](https://github.com/postgresql007)