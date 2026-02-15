# Monitoring Makefile
# Convenience commands for Temporal & Application monitoring

.PHONY: help
help: ## Show this help message
	@echo "Monitoring Management Commands"
	@echo "==============================="
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

##@ Access UIs

.PHONY: grafana
grafana: ## Start Grafana port-forward (http://localhost:3000)
	@echo "Starting Grafana port-forward..."
	@pkill -f 'port-forward.*grafana' 2>/dev/null || true
	@kubectl port-forward -n default svc/temporal-grafana 3000:80 > /dev/null 2>&1 &
	@sleep 2
	@echo "✅ Grafana available at http://localhost:3000"
	@echo "   Username: admin"
	@echo "   Password: Run 'make print-grafana-password'"

.PHONY: prometheus
prometheus: ## Start Prometheus port-forward (http://localhost:9090)
	@echo "Starting Prometheus port-forward..."
	@pkill -f 'port-forward.*prometheus-server' 2>/dev/null || true
	@kubectl port-forward -n default svc/temporal-prometheus-server 9090:80 > /dev/null 2>&1 &
	@sleep 2
	@echo "✅ Prometheus available at http://localhost:9090"

.PHONY: alertmanager
alertmanager: ## Start AlertManager port-forward (http://localhost:9093)
	@echo "Starting AlertManager port-forward..."
	@pkill -f 'port-forward.*alertmanager' 2>/dev/null || true
	@kubectl port-forward -n default svc/temporal-alertmanager 9093:9093 > /dev/null 2>&1 &
	@sleep 2
	@echo "✅ AlertManager available at http://localhost:9093"

.PHONY: worker-metrics
worker-metrics: ## Start worker metrics port-forward (http://localhost:9000/metrics)
	@echo "Starting worker metrics port-forward..."
	@pkill -f 'port-forward.*forohtoo-worker-metrics' 2>/dev/null || true
	@kubectl port-forward -n default svc/forohtoo-worker-metrics 9000:9000 > /dev/null 2>&1 &
	@sleep 2
	@echo "✅ Worker metrics available at http://localhost:9000/metrics"

.PHONY: all-uis
all-uis: grafana prometheus alertmanager worker-metrics ## Start all monitoring UIs
	@echo ""
	@echo "✅ All monitoring UIs started:"
	@echo "   Grafana:      http://localhost:3000 (user: admin, run 'make print-grafana-password')"
	@echo "   Prometheus:   http://localhost:9090"
	@echo "   AlertManager: http://localhost:9093"
	@echo "   Worker:       http://localhost:9000/metrics"

.PHONY: stop-all
stop-all: ## Stop all port-forwards
	@echo "Stopping all port-forwards..."
	@pkill -f 'kubectl port-forward' 2>/dev/null || true
	@echo "✅ All port-forwards stopped"

##@ Credentials

.PHONY: print-grafana-password
print-grafana-password: ## Print Grafana admin password
	@echo "Grafana Admin Password:"
	@kubectl get secret -n default temporal-grafana -o jsonpath="{.data.admin-password}" 2>/dev/null | base64 -d && echo || echo "Error: Could not retrieve password"

.PHONY: print-grafana-creds
print-grafana-creds: ## Print Grafana credentials (user + password)
	@echo "Grafana Credentials:"
	@echo "  Username: admin"
	@echo -n "  Password: "
	@kubectl get secret -n default temporal-grafana -o jsonpath="{.data.admin-password}" 2>/dev/null | base64 -d && echo || echo "Error"

##@ Kubernetes Resources

.PHONY: apply-alerts
apply-alerts: ## Apply Prometheus alert rules
	@echo "Applying Prometheus alert rules..."
	@kubectl get configmap temporal-prometheus-server -n default -o json | \
		jq '.data."forohtoo_alerts.yml" = (input | @text)' k8s/prometheus-alert-rules.yaml | \
		kubectl apply -f - 2>&1 | grep -v "Warning: resource.*missing.*annotation" || true
	@echo "✅ Alert rules applied"
	@echo "⚠️  Run 'make reload-prometheus' to activate"

.PHONY: apply-service
apply-service: ## Apply worker metrics service
	@echo "Applying worker metrics service..."
	@kubectl apply -f k8s/worker-metrics-service.yaml
	@echo "✅ Service applied"

.PHONY: apply-all
apply-all: apply-service apply-alerts ## Apply all Kubernetes resources
	@echo "✅ All resources applied"

##@ Prometheus Management

.PHONY: reload-prometheus
reload-prometheus: ## Reload Prometheus configuration (after updating rules)
	@echo "Reloading Prometheus..."
	@kubectl exec -n default deployment/temporal-prometheus-server -c prometheus-server -- killall -HUP prometheus 2>/dev/null || \
		echo "⚠️  Failed to send reload signal. Prometheus may need restart."
	@sleep 3
	@echo "✅ Prometheus reload signal sent"

.PHONY: restart-prometheus
restart-prometheus: ## Restart Prometheus pod (full restart)
	@echo "Restarting Prometheus..."
	@kubectl delete pod -n default -l app.kubernetes.io/name=prometheus,app.kubernetes.io/component=server
	@echo "Waiting for pod to restart..."
	@kubectl wait --for=condition=ready pod -l app.kubernetes.io/name=prometheus,app.kubernetes.io/component=server -n default --timeout=60s
	@echo "✅ Prometheus restarted"

.PHONY: show-prometheus-config
show-prometheus-config: ## Show current Prometheus configuration
	@kubectl exec -n default deployment/temporal-prometheus-server -c prometheus-server -- cat /etc/config/prometheus.yml

.PHONY: check-storage
check-storage: ## Check Prometheus storage usage
	@echo "Prometheus Storage Usage:"
	@kubectl exec -n default deployment/temporal-prometheus-server -c prometheus-server -- df -h /data | tail -1

##@ Status Checks

.PHONY: check-alerts
check-alerts: ## Check current alert status
	@echo "Current Alerts Status:"
	@kubectl exec -n default deployment/temporal-prometheus-server -c prometheus-server -- \
		wget -qO- "http://localhost:9090/api/v1/rules" 2>&1 | \
		jq -r '.data.groups[] | select(.name == "forohtoo_workflows") | {name: .name, alerts: [.rules[] | {alert: .name, state: .state, severity: .labels.severity}]}'

.PHONY: check-targets
check-targets: ## Check Prometheus scrape targets
	@echo "Prometheus Targets:"
	@kubectl exec -n default deployment/temporal-prometheus-server -c prometheus-server -- \
		wget -qO- "http://localhost:9090/api/v1/targets" 2>&1 | \
		jq -r '.data.activeTargets[] | select(.labels.pod | contains("forohtoo")) | {pod: .labels.pod, health: .health, scrapeUrl: .scrapeUrl, lastScrape: .lastScrape}'

.PHONY: check-worker-annotations
check-worker-annotations: ## Check worker pod Prometheus annotations
	@echo "Worker Pod Annotations:"
	@kubectl get pod -l app=forohtoo-worker -n default -o jsonpath='{.items[0].metadata.annotations}' | jq '.'

.PHONY: check-datasource
check-datasource: ## Check Grafana Prometheus datasource
	@echo "Grafana Datasources:"
	@curl -s -u admin:$$(kubectl get secret -n default temporal-grafana -o jsonpath="{.data.admin-password}" | base64 -d) \
		"http://localhost:3000/api/datasources" 2>/dev/null | \
		jq -r '.[] | select(.type == "prometheus") | {name: .name, uid: .uid, isDefault: .isDefault}' || \
		echo "⚠️  Start Grafana port-forward first: make grafana"

.PHONY: status
status: ## Show overall monitoring status
	@echo "=== Monitoring Status ==="
	@echo ""
	@echo "Prometheus:"
	@kubectl get pod -l app.kubernetes.io/name=prometheus,app.kubernetes.io/component=server -n default -o wide 2>/dev/null || echo "  Not found"
	@echo ""
	@echo "Grafana:"
	@kubectl get pod -l app.kubernetes.io/name=grafana -n default -o wide 2>/dev/null || echo "  Not found"
	@echo ""
	@echo "AlertManager:"
	@kubectl get pod -l app.kubernetes.io/name=alertmanager -n default -o wide 2>/dev/null || echo "  Not found"
	@echo ""
	@echo "Worker Metrics Service:"
	@kubectl get svc forohtoo-worker-metrics -n default 2>/dev/null || echo "  Not found (run 'make apply-service')"
	@echo ""
	@echo "Worker Pods:"
	@kubectl get pod -l app=forohtoo-worker -n default 2>/dev/null || echo "  Not found"

##@ Grafana Management

.PHONY: import-dashboard
import-dashboard: ## Import forohtoo workflows dashboard to Grafana
	@echo "Importing dashboard..."
	@curl -s -X POST -H "Content-Type: application/json" \
		-u admin:$$(kubectl get secret -n default temporal-grafana -o jsonpath="{.data.admin-password}" | base64 -d) \
		"http://localhost:3000/api/dashboards/db" \
		-d @grafana/forohtoo-workflows.json | jq -r '.status, .url' || \
		echo "⚠️  Start Grafana port-forward first: make grafana"

.PHONY: list-dashboards
list-dashboards: ## List all Grafana dashboards
	@echo "Grafana Dashboards:"
	@curl -s -u admin:$$(kubectl get secret -n default temporal-grafana -o jsonpath="{.data.admin-password}" | base64 -d) \
		"http://localhost:3000/api/search" 2>/dev/null | \
		jq -r '.[] | "\(.title) - http://localhost:3000\(.url)"' || \
		echo "⚠️  Start Grafana port-forward first: make grafana"

.PHONY: export-dashboard
export-dashboard: ## Export forohtoo dashboard from Grafana
	@echo "Exporting dashboard..."
	@curl -s -u admin:$$(kubectl get secret -n default temporal-grafana -o jsonpath="{.data.admin-password}" | base64 -d) \
		"http://localhost:3000/api/dashboards/uid/ffda71shyzvggb" 2>/dev/null | \
		jq '.dashboard' > grafana/forohtoo-workflows-exported.json && \
		echo "✅ Exported to grafana/forohtoo-workflows-exported.json" || \
		echo "⚠️  Start Grafana port-forward first: make grafana"

##@ Logs

.PHONY: logs-prometheus
logs-prometheus: ## Tail Prometheus logs
	@kubectl logs -n default -f deployment/temporal-prometheus-server -c prometheus-server

.PHONY: logs-grafana
logs-grafana: ## Tail Grafana logs
	@kubectl logs -n default -f deployment/temporal-grafana

.PHONY: logs-alertmanager
logs-alertmanager: ## Tail AlertManager logs
	@kubectl logs -n default -f statefulset/temporal-alertmanager -c alertmanager

.PHONY: logs-worker
logs-worker: ## Tail worker logs
	@kubectl logs -n default -f -l app=forohtoo-worker

##@ Development

.PHONY: test-alerts
test-alerts: ## Validate alert rule syntax
	@echo "Validating alert rules..."
	@docker run --rm -v $$(pwd)/k8s:/rules prom/prometheus:latest promtool check rules /rules/prometheus-alert-rules.yaml || \
		echo "⚠️  Install promtool to validate locally"

.PHONY: query
query: ## Run PromQL query (usage: make query QUERY='up')
	@kubectl exec -n default deployment/temporal-prometheus-server -c prometheus-server -- \
		wget -qO- "http://localhost:9090/api/v1/query?query=$$(echo '$(QUERY)' | sed 's/ /%20/g')" 2>&1 | jq -r '.data.result'

##@ Cleanup

.PHONY: delete-alerts
delete-alerts: ## Remove forohtoo alert rules from Prometheus
	@echo "Removing alert rules..."
	@kubectl get configmap temporal-prometheus-server -n default -o json | \
		jq 'del(.data."forohtoo_alerts.yml")' | kubectl apply -f -
	@echo "✅ Alert rules removed. Run 'make reload-prometheus' to apply"

.PHONY: delete-service
delete-service: ## Delete worker metrics service
	@kubectl delete -f k8s/worker-metrics-service.yaml 2>/dev/null || echo "Service not found"

##@ Documentation

.PHONY: docs
docs: ## Open detailed monitoring guide
	@cat docs/DETAILED-GUIDE.md | less

.PHONY: troubleshoot
troubleshoot: ## Open troubleshooting guide
	@cat docs/TROUBLESHOOTING.md | less
