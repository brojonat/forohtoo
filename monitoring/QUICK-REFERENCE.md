# Quick Reference Cheat Sheet

Essential commands for day-to-day monitoring operations.

## 🚀 Quick Start

```bash
make all-uis              # Start all UIs at once
make print-grafana-creds  # Get login info
```

## 🔐 Credentials

```bash
make print-grafana-password    # Just the password
make print-grafana-creds       # Username + password
```

**Login:** admin / (password from command above)

## 🌐 Access URLs

| Service | Command | URL |
|---------|---------|-----|
| Grafana | `make grafana` | http://localhost:3000 |
| Prometheus | `make prometheus` | http://localhost:9090 |
| AlertManager | `make alertmanager` | http://localhost:9093 |
| Worker Metrics | `make worker-metrics` | http://localhost:9000/metrics |

## 📊 Dashboards

**Forohtoo Workflows:**
- URL: http://localhost:3000/d/ffda71shyzvggb/forohtoo-workflows
- Shows: Activity execution times, rates, wallet count

**List all:**
```bash
make list-dashboards
```

## 🚨 Alerts

```bash
make check-alerts       # View current alert status
make apply-alerts       # Apply alert rules
make reload-prometheus  # Activate new rules
```

## 🔍 Status Checks

```bash
make status             # Overall system status
make check-targets      # What Prometheus is scraping
make check-storage      # Disk usage
```

## 📝 Logs

```bash
make logs-prometheus    # Prometheus logs
make logs-grafana       # Grafana logs
make logs-alertmanager  # AlertManager logs
make logs-worker        # Worker logs
```

## 🛠️ Management

```bash
make reload-prometheus   # Reload config (soft)
make restart-prometheus  # Restart pod (hard)
make stop-all           # Stop all port-forwards
```

## 📦 Apply Resources

```bash
make apply-service      # Worker metrics service
make apply-alerts       # Alert rules
make apply-all          # Everything
```

## 💾 Grafana

```bash
make import-dashboard   # Import custom dashboard
make export-dashboard   # Export current version
```

## 🐛 Troubleshooting

```bash
make check-worker-annotations  # Verify Prometheus can discover worker
make check-datasource         # Verify Grafana → Prometheus connection
make check-targets            # See scrape health
```

**Common fixes:**
```bash
# Dashboard not loading
make grafana
make import-dashboard

# Metrics not showing
make check-targets
make worker-metrics  # Test direct access

# Alerts not working
make apply-alerts
make reload-prometheus
make check-alerts
```

## 📖 Documentation

```bash
make help           # Show all commands
make docs           # Read detailed guide
make troubleshoot   # Open troubleshooting guide
```

Or read files directly:
- **README.md** - Overview & quick start
- **docs/DETAILED-GUIDE.md** - Complete documentation
- **docs/TROUBLESHOOTING.md** - Common issues

## 🔧 Useful PromQL Queries

**Activity p95 latency:**
```promql
histogram_quantile(0.95, rate(poll_activity_duration_seconds_bucket[5m]))
```

**Execution rate (per minute):**
```promql
sum by (activity) (rate(poll_activity_duration_seconds_count[5m])) * 60
```

**Total executions (last hour):**
```promql
sum(increase(poll_activity_duration_seconds_count[1h]))
```

**Active wallets:**
```promql
count(count by (wallet_address) (poll_activity_duration_seconds_count))
```

## 🎯 Common Workflows

### First Time Setup

```bash
# 1. Apply resources
make apply-all

# 2. Start UIs
make all-uis

# 3. Get Grafana password
make print-grafana-password

# 4. Access Grafana
open http://localhost:3000

# 5. Import dashboard (if needed)
make import-dashboard
```

### Daily Operations

```bash
# Check if everything is healthy
make status

# View metrics
make grafana
# → http://localhost:3000/d/ffda71shyzvggb/forohtoo-workflows

# Check for firing alerts
make check-alerts
```

### After Config Changes

```bash
# Alert rules changed
make apply-alerts
make reload-prometheus

# Dashboard changed
make import-dashboard

# Worker deployment changed
kubectl rollout restart deployment forohtoo-worker -n default
# Wait 2 minutes for Prometheus to rediscover
```

### Debugging Issues

```bash
# Full diagnostic
make status
make check-targets
make check-storage
make logs-prometheus | tail -50

# Check worker metrics directly
make worker-metrics
curl http://localhost:9000/metrics | grep poll_activity

# Check Prometheus scraping
make prometheus
# → http://localhost:9090/targets
```

## 🔑 Key Files

| File | Purpose |
|------|---------|
| `Makefile` | All commands |
| `README.md` | Overview & guide |
| `k8s/prometheus-alert-rules.yaml` | Alert definitions |
| `k8s/worker-metrics-service.yaml` | Metrics service |
| `grafana/forohtoo-workflows.json` | Custom dashboard |
| `docs/DETAILED-GUIDE.md` | Full documentation |
| `docs/TROUBLESHOOTING.md` | Debug guide |

## 🎓 Learning Resources

**In Grafana:**
- Explore → Metrics browser (see all available metrics)
- Dashboards → Browse (see pre-built dashboards)
- Alerting → Alert rules (view Temporal alerts)

**In Prometheus:**
- http://localhost:9090/graph (query editor)
- http://localhost:9090/targets (scrape targets)
- http://localhost:9090/alerts (alert status)
- http://localhost:9090/config (view configuration)

**In AlertManager:**
- http://localhost:9093/#/alerts (active alerts)
- http://localhost:9093/#/silences (muted alerts)
- http://localhost:9093/#/status (configuration)

---

💡 **Tip:** Run `make help` anytime to see all available commands!
