# Operations Guide

Day-2 operations for k8zner clusters on Hetzner Cloud.

## Scaling Workers

### Scale Up

Edit `k8zner.yaml` and increase the worker count:

```yaml
workers:
  count: 5  # was 3
  size: cx33
```

Apply the change:

```bash
k8zner apply
```

The operator detects the desired count change and provisions new workers automatically. New nodes boot from a Talos snapshot, receive machine config, and join the cluster. Allow 3-5 minutes per node.

### Scale Down

Decrease the worker count in `k8zner.yaml` and apply:

```bash
k8zner apply
```

The operator selects workers for removal (preferring unhealthy or newest nodes), cordons them, drains pods, deletes the Kubernetes node object, and deletes the Hetzner server.

### Monitor Scaling Progress

```bash
# Watch node status
kubectl get nodes -w

# Check operator events
kubectl get events -n k8zner-system --sort-by=.lastTimestamp

# Check cluster resource status
kubectl get k8znerclusters -o wide
```

## Scaling Control Planes

Control plane count is determined by the cluster mode:
- `dev`: 1 control plane (no scaling)
- `ha`: 3 control planes (no scaling below 3)

Switching from `dev` to `ha` mode is not supported as a live migration. To move to HA, create a new HA cluster and migrate workloads.

### Control Plane Self-Healing

In `ha` mode, the operator monitors control plane health. If a control plane node becomes unhealthy beyond the configured threshold:

1. Operator checks etcd quorum (needs majority healthy)
2. Creates a replacement server from the Talos snapshot
3. Generates and applies a new Talos machine config
4. Waits for the node to join the cluster and become ready
5. Removes the old unhealthy node (only after replacement is healthy)

This process is fully automatic. Monitor via:

```bash
kubectl get events -n k8zner-system | grep -i "control-plane\|healing\|quorum"
```

## Addon Upgrades

The operator keeps addons converged on the chart versions pinned in the
running operator release. When you upgrade the operator, outdated addons are
re-rendered and re-applied one at a time, only while every desired node is
ready, with events (`AddonUpgrading`, `AddonUpgraded`, `AddonUpgradeFailed`)
and per-addon versions visible in `status.addons`. Addons enabled in the spec
after provisioning are installed the same way.

Cilium is deliberately excluded from automatic upgrades: CNI upgrades affect
every pod on the cluster and remain a manual operation.

The `UpToDate` condition reports Kubernetes version skew: if any node's
kubelet does not match `spec.kubernetes.version`, the condition turns False
with an `UpgradePending` event. The operator never replaces nodes for version
reasons — run `k8zner apply` to roll out node upgrades.

## Sharing Read-Only Access

Generate a kubeconfig scoped to the built-in `view` ClusterRole (no access to
Secrets):

```bash
k8zner kubeconfig --read-only                 # token valid 1 year
k8zner kubeconfig --read-only --duration 720h -o ./readonly.yaml
```

The credential is a ServiceAccount token; revoke access by deleting the
`k8zner-viewer` ClusterRoleBinding.

## Upgrading Kubernetes

k8zner pins Kubernetes and Talos versions in its version matrix. To upgrade:

1. Update to the latest k8zner release (which includes the new version matrix)
2. Run `k8zner apply` to roll out the upgrade

The upgrade process:
1. Creates a new Talos snapshot with the updated versions
2. Replaces control plane nodes one at a time (maintaining etcd quorum)
3. Replaces worker nodes with rolling updates

**Important**: Always back up etcd before upgrading. See [Backup and Restore](#backup-and-restore).

## Backup and Restore

### Enabling Backups

Add `backup: true` to your config and provide S3 credentials:

```bash
export HETZNER_S3_ACCESS_KEY="your-access-key"
export HETZNER_S3_SECRET_KEY="your-secret-key"
k8zner apply
```

This creates:
- An S3 bucket: `{cluster-name}-etcd-backups`
- A CronJob running hourly etcd snapshots
- Compressed backup files stored in Hetzner Object Storage

### Checking Backup Status

```bash
# List recent backup jobs
kubectl get jobs -n kube-system -l app=talos-backup --sort-by=.status.startTime

# Check the latest job logs
kubectl logs -n kube-system job/$(kubectl get jobs -n kube-system -l app=talos-backup \
  --sort-by=.status.startTime -o jsonpath='{.items[-1].metadata.name}')

# List backups in S3 (requires AWS CLI configured for Hetzner)
aws s3 ls s3://{cluster-name}-etcd-backups/ \
  --endpoint-url https://fsn1.your-objectstorage.com
```

### Restore from Backup

To restore etcd from a backup (disaster recovery):

```bash
# 1. Download the backup
aws s3 cp s3://{cluster-name}-etcd-backups/{backup-file} ./backup.snapshot \
  --endpoint-url https://fsn1.your-objectstorage.com

# 2. Bootstrap a new cluster with the backup
talosctl bootstrap --recover-from=./backup.snapshot --nodes <control-plane-ip>
```

**Note**: Restore creates a new etcd cluster from the snapshot. All nodes will need to rejoin.

### Backup Bucket Cleanup

When you destroy a cluster, the backup bucket is intentionally preserved to prevent accidental data loss. To delete it manually:

```bash
aws s3 rb s3://{cluster-name}-etcd-backups --force \
  --endpoint-url https://fsn1.your-objectstorage.com
```

## Health Monitoring

### Cluster Health

The operator continuously monitors cluster health:

```bash
# Overall cluster status
kubectl get k8znerclusters -o wide

# Detailed cluster conditions
kubectl get k8znerclusters -o jsonpath='{.items[0].status.conditions}' | jq .

# Node health
kubectl get nodes -o wide
```

### Control Plane Health

```bash
# Check etcd health via Talos
talosctl etcd status --nodes <control-plane-ip>

# Check etcd member list
talosctl etcd members --nodes <control-plane-ip>

# Check Kubernetes API health
kubectl get componentstatuses
```

### Addon Health

```bash
# Check all addon pods
kubectl get pods -n kube-system
kubectl get pods -n cert-manager
kubectl get pods -n traefik
kubectl get pods -n argocd

# Check addon status in cluster resource
kubectl get k8znerclusters -o jsonpath='{.items[0].status.addons}' | jq .
```

### Monitoring Stack

When `monitoring: true` is configured:

```bash
# Access Grafana (if domain is configured)
open https://grafana.example.com

# Port-forward Grafana locally
kubectl port-forward -n monitoring svc/kube-prometheus-stack-grafana 3000:80

# Port-forward Prometheus locally
kubectl port-forward -n monitoring svc/kube-prometheus-stack-prometheus 9090:9090
```

Default Grafana credentials are managed by the Helm chart. Check the Grafana secret:

```bash
kubectl get secret -n monitoring kube-prometheus-stack-grafana -o jsonpath='{.data.admin-password}' | base64 -d
```

## Diagnosing Issues

The `doctor` command provides a quick overview of cluster health:

```bash
# Pre-cluster: validate config and show what would be created
k8zner doctor

# With a running cluster: show infrastructure, node, and addon status
k8zner doctor

# Continuous monitoring (refreshes every 5 seconds)
k8zner doctor --watch

# Machine-readable output
k8zner doctor --json
```

The output includes emoji indicators for each component's status and highlights any issues that need attention.

## Destroying a Cluster

```bash
k8zner destroy
```

This removes all Hetzner Cloud resources (servers, networks, firewalls, load balancers, snapshots, SSH keys). S3 backup buckets are preserved.

**Warning**: This is irreversible. Ensure you have backups if needed.

### Why `kubectl delete k8znercluster` does not destroy infrastructure

Deleting the `K8znerCluster` resource intentionally leaves all cloud
infrastructure intact — there is no finalizer that tears down Hetzner
resources. This is a deliberate design decision, not an omission: the
operator runs *inside* the cluster it manages, so a finalizer-driven
teardown would delete the servers the operator itself runs on partway
through cleanup, reliably leaving half-deleted infrastructure behind with
nothing left to finish the job. Always tear down clusters from outside
with `k8zner destroy` (or the `cleanup` utility for orphaned resources).

## Talos Administration

k8zner clusters run Talos Linux. Common Talos operations:

```bash
# Get talosconfig (generated during bootstrap)
export TALOSCONFIG=./talosconfig

# Check node health
talosctl health --nodes <node-ip>

# View system logs
talosctl logs kubelet --nodes <node-ip>
talosctl logs etcd --nodes <node-ip>

# View running services
talosctl services --nodes <node-ip>

# Get kernel logs
talosctl dmesg --nodes <node-ip>

# List network interfaces
talosctl get links --nodes <node-ip>
```

## See Also

- [Configuration Guide](configuration.md)
- [Troubleshooting](troubleshooting.md)
- [Architecture](architecture.md)
