# Temporal Monitoring & Alerting Guide

This guide covers how to monitor your Temporal workflows and set up alerting for failures.

## Overview

Your Temporal Helm chart deployed a complete monitoring stack:
- **Grafana**: Visualization and dashboards (`temporal-grafana`)
- **Prometheus**: Metrics collection (`temporal-prometheus-server`)
- **AlertManager**: Alert routing and notifications (`temporal-alertmanager`)

## Accessing Grafana

**Port-forward to Grafana:**
```bash
kubectl port-forward -n default svc/temporal-grafana 3000:80
```

**Login Credentials:**
- URL: `http://localhost:3000`
- Username: `admin`
- Password: Get it with:
  ```bash
  kubectl get secret -n default temporal-grafana -o jsonpath="{.data.admin-password}" | base64 -d && echo
  ```

**Pre-built Dashboard:**
- Navigate to "Temporal Server Metrics" dashboard (ID: 2)
- Shows server-level metrics: workflow completion rates, latencies, queue depths

## Available Metrics

### 1. Temporal Server Metrics (from Temporal services)

Temporal server exposes metrics at the **namespace** level. Key metrics:

**Workflow Execution Metrics:**
- `temporal_workflow_completed` - Completed workflows count
- `temporal_workflow_failed` - Failed workflows count
- `temporal_workflow_canceled` - Canceled workflows count
- `temporal_workflow_terminated` - Terminated workflows count
- `temporal_workflow_timeout` - Timed out workflows count
- `temporal_workflow_continue_as_new` - Workflows that continued as new

**Labels available:**
- `namespace` - Temporal namespace (e.g., "forohtoo")
- `task_queue` - Task queue name
- `workflow_type` - Workflow type name

**Latency Metrics:**
- `temporal_workflow_endtoend_latency` - E2E workflow execution time
- `temporal_workflow_task_execution_latency` - Task execution time
- `temporal_workflow_task_schedule_to_start_latency` - Scheduling delay

### 2. Custom Application Metrics (from your workers)

Your workers expose custom metrics via the `/metrics` endpoint on port 9091:

**Currently exposed:**
- `forohtoo_rpc_call_duration_seconds` - RPC call latency by endpoint
- `forohtoo_rpc_call_total` - RPC call count by status/endpoint
- `forohtoo_rpc_rate_limit_hits_total` - Rate limit hits by endpoint
- `forohtoo_rpc_retries_total` - Retry count by reason
- `forohtoo_rpc_signatures_per_call` - Signatures fetched per call
- `forohtoo_transactions_parsed_total` - Transaction parse results
- `forohtoo_transactions_skipped_total` - Skipped transactions by reason
- `forohtoo_activity_duration_seconds` - Temporal activity execution time

**Worker metrics endpoint:**
```bash
kubectl port-forward -n default svc/forohtoo-worker 9091:9091
curl http://localhost:9091/metrics
```

## Alerting Strategies

You have **three options** for alerting on failed workflows:

### Option 1: Prometheus AlertManager Rules (Recommended)

Configure AlertManager rules to alert on workflow failures.

**Create alert rules file:** `k8s/prometheus-alerts.yaml`

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: temporal-prometheus-alerts
  namespace: default
data:
  alerts.yml: |
    groups:
    - name: temporal_workflows
      interval: 30s
      rules:
      # Alert on high workflow failure rate
      - alert: HighWorkflowFailureRate
        expr: |
          rate(temporal_workflow_failed{namespace="forohtoo"}[5m]) > 0.1
        for: 2m
        labels:
          severity: warning
          namespace: forohtoo
        annotations:
          summary: "High workflow failure rate in {{ $labels.namespace }}"
          description: "Workflow failure rate is {{ $value }} failures/sec in namespace {{ $labels.namespace }}, task queue {{ $labels.task_queue }}"

      # Alert on any workflow failures (if you want to be notified immediately)
      - alert: WorkflowFailed
        expr: |
          increase(temporal_workflow_failed{namespace="forohtoo"}[1m]) > 0
        labels:
          severity: info
          namespace: forohtoo
        annotations:
          summary: "Workflow failed in {{ $labels.namespace }}"
          description: "{{ $value }} workflow(s) failed in the last minute in namespace {{ $labels.namespace }}"

      # Alert on high RPC rate limit hits
      - alert: HighRPCRateLimitHits
        expr: |
          rate(forohtoo_rpc_rate_limit_hits_total[5m]) > 0.5
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High RPC rate limit hits on {{ $labels.endpoint }}"
          description: "Endpoint {{ $labels.endpoint }} is hitting rate limits at {{ $value }} hits/sec"

      # Alert on no successful workflow completions (system health check)
      - alert: NoWorkflowCompletions
        expr: |
          rate(temporal_workflow_completed{namespace="forohtoo"}[10m]) == 0
        for: 15m
        labels:
          severity: critical
          namespace: forohtoo
        annotations:
          summary: "No workflow completions in {{ $labels.namespace }}"
          description: "No workflows have completed in the last 15 minutes - possible system issue"
```

**Apply the alert rules:**
```bash
kubectl apply -f k8s/prometheus-alerts.yaml

# Reload Prometheus configuration
kubectl exec -n default temporal-prometheus-server-<pod-id> -- killall -HUP prometheus
```

**Configure AlertManager for notifications:**

Edit AlertManager config to send alerts to Slack, PagerDuty, email, etc:

```bash
kubectl edit configmap temporal-alertmanager -n default
```

Example Slack integration:
```yaml
global:
  slack_api_url: 'https://hooks.slack.com/services/YOUR/WEBHOOK/URL'

route:
  receiver: 'slack-notifications'
  group_by: ['alertname', 'namespace']
  group_wait: 10s
  group_interval: 5m
  repeat_interval: 3h

receivers:
- name: 'slack-notifications'
  slack_configs:
  - channel: '#temporal-alerts'
    title: '{{ range .Alerts }}{{ .Annotations.summary }}{{ end }}'
    text: '{{ range .Alerts }}{{ .Annotations.description }}{{ end }}'
```

### Option 2: Query Temporal Visibility Store (SQL-based)

The Temporal visibility store (PostgreSQL) contains all workflow execution data. You can query it directly for alerting:

```sql
-- Count failed workflows in the last hour by workflow type
SELECT
  workflow_type_name,
  COUNT(*) as failure_count
FROM executions_visibility
WHERE
  namespace = 'forohtoo'
  AND status = 'Failed'
  AND close_time > NOW() - INTERVAL '1 hour'
GROUP BY workflow_type_name
ORDER BY failure_count DESC;

-- Get details of recent failed workflows
SELECT
  workflow_id,
  run_id,
  workflow_type_name,
  close_time,
  execution_status,
  history_length,
  task_queue
FROM executions_visibility
WHERE
  namespace = 'forohtoo'
  AND status = 'Failed'
  AND close_time > NOW() - INTERVAL '24 hours'
ORDER BY close_time DESC
LIMIT 20;
```

**Build a custom alerting service:**
1. Query the visibility store periodically (every 1-5 minutes)
2. Track failure counts by workflow type
3. Send alerts when thresholds are exceeded
4. Store in your existing PostgreSQL database for historical tracking

### Option 3: Temporal Cloud Visibility API (Recommended for production)

Use Temporal's gRPC API to query workflow execution history:

```go
// Example: Query for failed workflows in the last hour
package main

import (
    "context"
    "log"
    "time"

    "go.temporal.io/api/enums/v1"
    "go.temporal.io/api/workflowservice/v1"
    "go.temporal.io/sdk/client"
)

func countFailedWorkflows(ctx context.Context, c client.Client) (int64, error) {
    query := "ExecutionStatus='Failed' AND CloseTime > '" +
        time.Now().Add(-1*time.Hour).Format(time.RFC3339) + "'"

    var count int64
    var nextPageToken []byte

    for {
        resp, err := c.WorkflowService().ListWorkflowExecutions(ctx, &workflowservice.ListWorkflowExecutionsRequest{
            Namespace: "forohtoo",
            Query:     query,
            PageSize:  1000,
            NextPageToken: nextPageToken,
        })
        if err != nil {
            return 0, err
        }

        count += int64(len(resp.Executions))

        if len(resp.NextPageToken) == 0 {
            break
        }
        nextPageToken = resp.NextPageToken
    }

    return count, nil
}

func main() {
    c, err := client.Dial(client.Options{
        HostPort:  "localhost:7233",
        Namespace: "forohtoo",
    })
    if err != nil {
        log.Fatal(err)
    }
    defer c.Close()

    ctx := context.Background()
    count, err := countFailedWorkflows(ctx, c)
    if err != nil {
        log.Fatal(err)
    }

    log.Printf("Failed workflows in last hour: %d", count)

    // Alert if count exceeds threshold
    if count > 10 {
        // Send alert to Slack, PagerDuty, etc.
        log.Printf("ALERT: High workflow failure count: %d", count)
    }
}
```

Run this as a cron job or scheduled workflow to monitor failures.

## Recommended Approach

**For immediate setup:** Use **Option 1 (AlertManager)** because:
- Already deployed with your Temporal Helm chart
- No additional infrastructure needed
- Grafana integration for visualization
- Industry-standard alerting

**For long-term:** Combine **Option 1 + Option 3**:
1. Use AlertManager for real-time alerts on critical issues
2. Use Temporal API queries for detailed dashboards and analytics
3. Store workflow failure stats in your database for historical analysis

## Grafana Dashboard Setup

Create a custom dashboard for your workflows:

1. **Access Grafana** (http://localhost:3000)
2. **Create new dashboard**
3. **Add panels with these queries:**

**Failed Workflows (last 24h):**
```promql
increase(temporal_workflow_failed{namespace="forohtoo"}[24h])
```

**Workflow Failure Rate (per minute):**
```promql
rate(temporal_workflow_failed{namespace="forohtoo"}[5m]) * 60
```

**Failed Workflows by Type:**
```promql
sum by (workflow_type) (increase(temporal_workflow_failed{namespace="forohtoo"}[1h]))
```

**RPC Rate Limit Hits:**
```promql
sum by (endpoint) (increase(forohtoo_rpc_rate_limit_hits_total[1h]))
```

**Worker Activity Latency (p95):**
```promql
histogram_quantile(0.95,
  rate(forohtoo_activity_duration_seconds_bucket[5m])
)
```

## Exposing Grafana Externally

If you want to access Grafana without port-forwarding:

**Option 1: Kubernetes Ingress**
```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: temporal-grafana-ingress
  namespace: default
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt-prod
spec:
  tls:
  - hosts:
    - grafana.forohtoo.brojonat.com
    secretName: grafana-tls
  rules:
  - host: grafana.forohtoo.brojonat.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: temporal-grafana
            port:
              number: 80
```

**Option 2: Tailscale (like your Temporal Web UI)**
Similar to how you exposed Temporal Web, you can expose Grafana via Tailscale.

## Next Steps

1. **Access Grafana now:**
   ```bash
   kubectl port-forward -n default svc/temporal-grafana 3000:80
   # Visit http://localhost:3000
   ```

2. **Check existing dashboards** and see what's already available

3. **Decide on alerting strategy:**
   - Quick win: Set up AlertManager rules (30 minutes)
   - Long-term: Build custom monitoring service with Temporal API

4. **Set up notifications** (Slack, email, PagerDuty)

5. **Consider adding custom metrics** to your workers for domain-specific alerting

Would you like me to help you:
- Set up specific AlertManager rules?
- Create a custom Grafana dashboard?
- Build a monitoring service using the Temporal API?
- Configure Slack/email notifications?
