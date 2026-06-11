# Configuration Guide

This document describes the simplified configuration format for k8zner clusters.

## Quick Start

Create a configuration using the interactive wizard:

```bash
k8zner init
```

This generates a `k8zner.yaml` file. See [Interactive Wizard](wizard.md) for details.

## Configuration File

k8zner uses YAML configuration with an opinionated, minimal format:

```yaml
name: my-cluster
region: fsn1
mode: ha
workers:
  count: 3
  size: cx33
domain: example.com  # optional
```

That's it! All other settings use production-ready defaults.

## Configuration Fields

### name (required)

Cluster name. Must be 1-32 lowercase alphanumeric characters or hyphens.

```yaml
name: my-cluster
```

### region (required)

Hetzner datacenter region for all resources.

| Region | Code | Location |
|--------|------|----------|
| Falkenstein | `fsn1` | Germany |
| Nuremberg | `nbg1` | Germany |
| Helsinki | `hel1` | Finland |

```yaml
region: fsn1
```

### mode (required)

Cluster mode determines the topology:

| Mode | Control Planes | Load Balancers | Best For |
|------|----------------|----------------|----------|
| `dev` | 1 | 1 (shared API/ingress) | Development, testing |
| `ha` | 3 | 2 (dedicated API + ingress) | Production |

```yaml
mode: ha
```

### workers (required)

Worker node configuration:

| Field | Description | Valid Values |
|-------|-------------|--------------|
| `count` | Number of worker nodes | 1-5 |
| `size` | Worker server size | cx23-53, cpx22-52 |

```yaml
workers:
  count: 3
  size: cx33
```

**Why 1-5 workers?** The simplified config uses an opinionated limit to keep clusters predictable and cost-effective for initial deployment. For larger clusters, update the config and run `k8zner apply` again to scale workers.

#### Available Sizes

##### CX Series - Dedicated vCPU (Default)
Consistent performance, recommended for production:

| Size | vCPU | RAM | Best For |
|------|------|-----|----------|
| `cx23` | 2 | 4GB | Small workloads |
| `cx33` | 4 | 8GB | Medium workloads |
| `cx43` | 8 | 16GB | Larger workloads |
| `cx53` | 16 | 32GB | Heavy workloads |

##### CPX Series - Shared vCPU
Better availability, suitable for dev/test:

| Size | vCPU | RAM | Best For |
|------|------|-----|----------|
| `cpx22` | 2 | 4GB | Dev/test |
| `cpx32` | 4 | 8GB | Medium workloads |
| `cpx42` | 8 | 16GB | Larger workloads |
| `cpx52` | 16 | 32GB | Heavy workloads |

Note: Hetzner renamed server types in 2024 (cx22→cx23, etc.). Both old and new names are accepted for backwards compatibility.

### domain (optional)

Cloudflare-managed domain for automatic DNS and TLS certificates.

```yaml
domain: example.com
```

When set, this automatically enables:
- **external-dns**: Creates DNS records from Ingress resources
- **cert-manager Cloudflare DNS01**: Issues Let's Encrypt certificates
- **ArgoCD ingress**: Dashboard accessible at `argo.{domain}`

Requires `CF_API_TOKEN` environment variable.

### wildcard_certificate (optional)

Issue a single wildcard certificate for `*.{domain}` instead of per-host
certificates, and make it Traefik's default.

```yaml
domain: example.com
wildcard_certificate: true
```

Any ingress under the domain that does not name its own TLS secret is then
served with the wildcard certificate. Ingresses with an explicit TLS secret
are unaffected. Requires `domain`.

### logging (optional)

Install the log aggregation stack: Loki (single binary, persisted on a
Hetzner volume, 7-day retention) plus Grafana Alloy as the collector.

```yaml
logging: true
```

Alloy tails pod logs through the Kubernetes API — no privileged host mounts.
When `monitoring` is also enabled, Loki is registered as a Grafana datasource
automatically.

### cert_email (optional)

Email address for Let's Encrypt certificate expiration notifications.

```yaml
cert_email: ops@example.com
```

Let's Encrypt sends certificate expiration warnings (at 20 days, 10 days, and 1 day before expiry) to this email. **Recommended for production** to catch renewal failures.

If not set, defaults to `admin@{domain}`.

### argo_subdomain (optional)

Subdomain for the ArgoCD dashboard (default: `argo`).

```yaml
argo_subdomain: argocd
```

When `domain` is set, ArgoCD will be accessible at `{argo_subdomain}.{domain}`.
Example: with `domain: example.com` and `argo_subdomain: argocd`, ArgoCD is at `argocd.example.com`.

### backup (optional)

Enable automatic etcd backups to Hetzner Object Storage.

```yaml
backup: true
```

When enabled:
- Creates S3 bucket: `{cluster-name}-etcd-backups`
- Schedules hourly etcd snapshots via CronJob
- Stores compressed backups in Hetzner Object Storage

Requires environment variables:
```bash
export HETZNER_S3_ACCESS_KEY="your-access-key"
export HETZNER_S3_SECRET_KEY="your-secret-key"
```

**Note**: Backups are stored unencrypted in the private S3 bucket. The bucket is not publicly accessible. For additional security, enable server-side encryption in the Hetzner Cloud Console.

**Important - S3 Bucket Cleanup**: When you destroy a cluster with `k8zner destroy`, the S3 backup bucket is **intentionally NOT deleted** to prevent accidental data loss. This ensures your etcd backups remain available for disaster recovery or cluster migration.

To manually delete the bucket after destroying a cluster:
```bash
# Using AWS CLI (configured for Hetzner Object Storage)
aws s3 rb s3://{cluster-name}-etcd-backups --force \
  --endpoint-url https://{region}.your-objectstorage.com

# Or via Hetzner Cloud Console:
# 1. Navigate to Object Storage
# 2. Select the bucket "{cluster-name}-etcd-backups"
# 3. Delete all objects, then delete the bucket
```

## Opinionated Defaults

The simplified config automatically includes production-ready settings:

### Architecture & Regions
- **x86-64 only**: All nodes run on amd64 architecture (CX/CPX server types)
- **EU regions only**: Nuremberg, Falkenstein, Helsinki (where CX/CPX instances are available)
- **No ARM support**: CAX (Ampere) servers are not supported

### Infrastructure
- **IPv6-only nodes**: No public IPv4 (saves costs, better security)
- **Control planes**: cx23 servers by default (2 dedicated vCPU, 4GB RAM - sufficient for etcd + API server)
- **Disk encryption**: LUKS2 encryption for state and ephemeral partitions

### Networking
- **Cilium CNI**: eBPF-based networking with kube-proxy replacement
- **Tunnel mode (VXLAN)**: Reliable pod-to-pod communication on Hetzner Cloud
- **Hubble**: Network observability and monitoring

### Ingress
- **Traefik**: Modern ingress controller (Deployment + LoadBalancer service)
- **Hetzner Load Balancer**: CCM auto-creates LB via service annotations
- **cert-manager**: Automatic TLS certificates via Cloudflare DNS-01 challenge

### Load Balancers
- **API LB**: Provisioned in HA mode for Kubernetes API access
- **Ingress LB**: Auto-created by CCM when Traefik LoadBalancer service is deployed

### Addons (always enabled)
- Hetzner Cloud Controller Manager (CCM)
- Hetzner CSI Driver (volumes)
- Cilium (CNI)
- Traefik (ingress)
- cert-manager (TLS)
- Metrics Server (HPA/VPA)
- ArgoCD (GitOps)
- Gateway API CRDs
- Prometheus Operator CRDs

### Versions
The config uses a pinned, tested version matrix:
- Talos Linux: v1.9.0
- Kubernetes: v1.32.0

## Environment Variables

Required:
```bash
export HCLOUD_TOKEN="your-hetzner-api-token"
```

Optional (for DNS/TLS):
```bash
export CF_API_TOKEN="your-cloudflare-api-token"
```

Optional (for backups):
```bash
export HETZNER_S3_ACCESS_KEY="your-s3-access-key"
export HETZNER_S3_SECRET_KEY="your-s3-secret-key"
```

Get S3 credentials from [Hetzner Cloud Console](https://console.hetzner.cloud/) → Object Storage → Security Credentials.

## Example Configurations

### Development Cluster

Minimal cluster for testing:

```yaml
name: dev
region: fsn1
mode: dev
workers:
  count: 1
  size: cx23
```

**Cost**: ~€18/month (1 CP + 1 worker + 1 LB)

### Production HA Cluster

High-availability setup for production:

```yaml
name: production
region: fsn1
mode: ha
workers:
  count: 3
  size: cx33
domain: example.com
```

**Cost**: ~€64/month (3 CP + 3 workers + 2 LBs)

### Large Production Cluster

For heavy workloads:

```yaml
name: large-prod
region: fsn1
mode: ha
workers:
  count: 5
  size: cx53
domain: example.com
monitoring: true
backup: true
```

**Cost**: ~€180/month (3 CP + 5 workers + 2 LBs)

## File Location

k8zner auto-detects configuration in the current directory:

```bash
# Uses ./k8zner.yaml automatically
k8zner apply

# Or specify explicitly
k8zner apply -c /path/to/config.yaml
```

## See Also

- [Interactive Wizard](wizard.md)
- [Architecture](architecture.md)
