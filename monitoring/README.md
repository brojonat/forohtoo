# Temporal & Application Monitoring

Complete monitoring setup for Temporal workflows and forohtoo application metrics using Grafana, Prometheus, and AlertManager.

## Quick Start

### Access Monitoring UIs

```bash
# Grafana (username: admin)
make grafana
make print-grafana-password

# Prometheus
make prometheus

# AlertManager
make alertmanager

# Worker Metrics (raw endpoint)
make worker-metrics
```

Then visit:
- **Grafana:** http://localhost:3000
- **Prometheus:** http://localhost:9090
- **AlertManager:** http://localhost:9093
- **Worker Metrics:** http://localhost:9000/metrics

### View Dashboards

1. **Forohtoo Workflows Dashboard:**
   - Run `make grafana`
   - Navigate to: http://localhost:3000/d/ffda71shyzvggb/forohtoo-workflows

2. **Temporal Server Metrics:**
   - Pre-built dashboard showing Temporal server health
   - Shows internal workflow execution stats

### Apply Alert Rules

```bash
make apply-alerts
```

This configures Prometheus to monitor:
- Slow activity execution (p95 > 30s)
- No activity executions (system down)
- Low activity execution rate
- Very slow activities (p99 > 60s)

### Check Alert Status

```bash
make check-alerts
```

Shows which alerts are firing, pending, or inactive.

---

## What's Included

### Kubernetes Resources
- **`k8s/prometheus-alert-rules.yaml`** - Alert rules for worker activity monitoring
- **`k8s/worker-metrics-service.yaml`** - Service exposing worker metrics endpoint (port 9000)
- **`k8s/worker-deployment-patch.yaml`** - Deployment patch for Prometheus annotations

### Grafana Dashboards
- **`grafana/forohtoo-workflows.json`** - Custom dashboard for activity execution metrics

### Documentation
- **`docs/DETAILED-GUIDE.md`** - Comprehensive monitoring guide
  - All available metrics
  - Alert configuration strategies
  - Grafana dashboard setup
  - SQL queries for Temporal visibility
- **`docs/TROUBLESHOOTING.md`** - Common issues and solutions

---

## Architecture

```
┌─────────────────┐
│  Forohtoo       │
│  Worker Pods    │──┐
│  :9000/metrics  │  │
└─────────────────┘  │
                     │
                     │  scrapes every 60s
                     ▼
              ┌──────────────┐         ┌─────────────┐
              │  Prometheus  │────────▶│ AlertManager│
              │  :9090       │  alerts │ :9093       │
              └──────────────┘         └─────────────┘
                     │                        │
                     │                        │
                     ▼                        ▼
              ┌──────────────┐         ┌─────────────┐
              │   Grafana    │         │ Slack/Email │
              │   :3000      │         │ Webhooks    │
              └──────────────┘         └─────────────┘
```

### Metrics Flow

1. **Worker exposes metrics** on `:9000/metrics`
   - Activity execution durations (histograms)
   - Activity execution counts
   - Per-wallet, per-activity granularity

2. **Prometheus scrapes** worker pods every 60 seconds
   - Auto-discovery via pod annotations
   - 15-day retention (auto-cleanup)
   - 8GB storage

3. **Alert rules evaluate** every 30 seconds
   - Fire alerts to AlertManager when thresholds exceeded
   - Support for pending → firing states

4. **Grafana visualizes** time-series data
   - Pre-built dashboards
   - Custom queries
   - Real-time updates

---

## Metrics Reference

### Worker Metrics

All prefixed with `poll_activity_duration_seconds`:

```promql
# Activity execution latency (histogram)
poll_activity_duration_seconds_bucket{activity="PollSolana", wallet_address="..."}
poll_activity_duration_seconds_sum{activity="PollSolana", wallet_address="..."}
poll_activity_duration_seconds_count{activity="PollSolana", wallet_address="..."}

# Activities tracked:
# - GetExistingSignatures
# - PollSolana
# - WriteTransactions
# - (others as they execute)
```

### Useful Queries

```promql
# p95 latency by activity
histogram_quantile(0.95, rate(poll_activity_duration_seconds_bucket[5m]))

# Execution rate (per minute)
sum by (activity) (rate(poll_activity_duration_seconds_count[5m])) * 60

# Total executions in last hour
sum(increase(poll_activity_duration_seconds_count[1h]))

# Active wallets being monitored
count(count by (wallet_address) (poll_activity_duration_seconds_count))
```

---

## Alert Rules

| Alert | Severity | Threshold | Duration | Description |
|-------|----------|-----------|----------|-------------|
| **SlowActivityExecution** | warning | p95 > 30s | 5 min | Activities taking too long |
| **NoActivityExecutions** | critical | rate = 0 | 15 min | Worker stopped/stuck |
| **LowActivityExecutionRate** | warning | < 0.01/sec | 10 min | Very few executions |
| **VerySlowActivityExecution** | critical | p99 > 60s | 10 min | Severe performance issue |

---

## Configuration

### Worker Deployment

Workers must expose metrics and have Prometheus annotations:

```yaml
apiVersion: apps/v1
kind: Deployment
spec:
  template:
    metadata:
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "9000"
        prometheus.io/path: "/metrics"
    spec:
      containers:
      - name: worker
        ports:
        - name: metrics
          containerPort: 9000
```

### Prometheus Retention

Default: **15 days**, **8GB storage**

To change retention:
```bash
kubectl edit deployment temporal-prometheus-server -n default
# Update: --storage.tsdb.retention.time=30d
```

### AlertManager Notifications

See [`docs/DETAILED-GUIDE.md`](./docs/DETAILED-GUIDE.md#alertmanager-notifications) for:
- Slack integration
- Email setup
- PagerDuty integration
- Webhook configuration

---

## Common Tasks

### Import Grafana Dashboard

```bash
make import-dashboard
```

### Update Alert Rules

1. Edit `k8s/prometheus-alert-rules.yaml`
2. Run `make apply-alerts`
3. Run `make reload-prometheus`

### Check Prometheus Targets

```bash
make check-targets
```

Shows which endpoints Prometheus is scraping and their health.

### View Prometheus Config

```bash
make show-prometheus-config
```

### Restart Prometheus

```bash
make restart-prometheus
```

---

## Storage & Retention

### Prometheus Storage

- **Size:** 8GB persistent volume
- **Current usage:** Check with `make check-storage`
- **Retention:** 15 days (automatic cleanup)
- **Location:** `/data` in prometheus-server pod

### Will it fill up?

No. Here's why:
- Worker metrics: ~50 time series
- Scrape interval: 60 seconds
- Data points per day: ~72,000 per metric
- Compressed storage: ~5-10 MB/day
- 15-day retention means max ~150 MB for worker metrics
- Plenty of headroom in 8GB volume

---

## Troubleshooting

### Dashboard not loading?
```bash
make check-datasource
make import-dashboard
```

### Alerts not firing?
```bash
make check-alerts
make show-prometheus-config | grep -A5 "rule_files"
```

### Worker metrics not showing up?
```bash
make check-targets | grep forohtoo
make check-worker-annotations
```

For more, see [`docs/TROUBLESHOOTING.md`](./docs/TROUBLESHOOTING.md)

---

## Next Steps

1. **Set up notifications** - Configure AlertManager with Slack/email
2. **Create more dashboards** - Add RPC metrics, database metrics, etc.
3. **Tune alert thresholds** - Adjust based on your baseline performance
4. **Add more metrics** - Instrument additional parts of your application

See [`docs/DETAILED-GUIDE.md`](./docs/DETAILED-GUIDE.md) for complete documentation.

---

## Files

```
monitoring/
├── README.md                           # This file
├── Makefile                            # Convenience commands
├── docs/
│   ├── DETAILED-GUIDE.md              # Comprehensive guide
│   └── TROUBLESHOOTING.md             # Common issues
├── k8s/
│   ├── prometheus-alert-rules.yaml    # Alert rules
│   ├── worker-metrics-service.yaml    # Metrics service
│   └── worker-deployment-patch.yaml   # Deployment annotations
└── grafana/
    └── forohtoo-workflows.json        # Custom dashboard
```

---

## Contributing

When adding new metrics:
1. Update worker to expose metrics
2. Add to `docs/DETAILED-GUIDE.md` metrics reference
3. Create Grafana panels if needed
4. Add alert rules if appropriate
5. Document in this README

---

## Credits

Built for the forohtoo project monitoring Solana wallet activity via Temporal workflows.

- **Grafana:** Visualization
- **Prometheus:** Metrics storage & alerting
- **AlertManager:** Alert routing
- **Temporal:** Deployed via Helm chart (includes monitoring stack)
