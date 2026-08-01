# Documentation

## User Guides

| Document | Description |
|----------|-------------|
| [Configuration](configuration.md) | Full configuration reference — all YAML fields, defaults, and validation rules |
| [Operations](operations.md) | Day-2 operations — scaling, upgrades, backups, and self-healing |
| [Troubleshooting](troubleshooting.md) | Common issues with symptoms, root causes, and solutions |
| [Wizard](wizard.md) | Interactive setup guide for first-time users |

## Architecture

| Document | Description |
|----------|-------------|
| [Architecture](architecture.md) | System architecture — operator flow, phases, data flow, and project structure |
| [Cluster Operator Design](design/cluster-operator.md) | Technical design rationale for the operator-first approach |
| [Platform Vision](design/platform-vision.md) | Proposal: evolving k8zner into a Kubernetes-native infrastructure platform |
| [Platform Plan](plan/README.md) | Agent-ready implementation plan for the platform vision — work packages, conventions, epics |

## Examples

The [`examples/`](../examples/) directory contains ready-to-use configuration files:

| Example | Description |
|---------|-------------|
| [`dev.yaml`](../examples/dev.yaml) | Minimal single-node development cluster |
| [`ha-production.yaml`](../examples/ha-production.yaml) | HA cluster with 3 control planes |
| [`full-production.yaml`](../examples/full-production.yaml) | All features enabled (monitoring, backups, ArgoCD) |
| [`monitoring.yaml`](../examples/monitoring.yaml) | Prometheus + Grafana stack |
| [`backups.yaml`](../examples/backups.yaml) | Talos etcd backup to S3 |

## Contributing

| Document | Description |
|----------|-------------|
| [Contributing Guide](../CONTRIBUTING.md) | Development setup, code style, and commit format |
| [Code Structure](<../.claude/CODE_STRUCTURE.md>) | Architecture patterns, naming conventions, and package guidelines |
| [Security Policy](../SECURITY.md) | Vulnerability reporting and security best practices |
