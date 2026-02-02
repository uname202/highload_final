# highload_final

Go service for streaming metrics with Redis caching and Prometheus monitoring.

## Run

- Start Redis locally (default: `127.0.0.1:6379`).
- Build and run:

```
go run ./cmd
```

### Environment

- `HTTP_ADDR` (default `:8080`)
- `REDIS_ADDR` (default `127.0.0.1:6379`)
- `REDIS_PASSWORD` (default empty)
- `REDIS_DB` (default `0`)
- `ANALYTICS_WINDOW` (default `50`)
- `ANALYTICS_THRESHOLD` (default `2.0`)

## Endpoints

- `POST /ingest` JSON: `{ "timestamp": 1738500000, "cpu": 0.42, "rps": 120.5, "device_id": "dev-1" }`
- `GET /analytics`
- `GET /latest?device_id=dev-1`
- `GET /health`
- `GET /metrics` (Prometheus)

## Notes

- Rolling average and z-score are calculated over a sliding window of 50 events.
- Redis stores the latest metric and a recent list (last 1000 metrics).

## Minikube + Ingress + HPA

### 1) Build and load image into Minikube

```
eval $(minikube -p minikube docker-env)
docker build -t highload:latest .
```

### 2) Install Redis with Helm (bitnami/redis)

```
helm repo add bitnami https://charts.bitnami.com/bitnami
helm repo update
helm install redis bitnami/redis --set auth.enabled=false
```

The Go deployment expects Redis at `redis-master:6379`.

### 3) Deploy Go app + Service

```
kubectl apply -f k8s/metrics-app.yaml
```

### 4) Ingress for /metrics and /analytics

```
kubectl apply -f k8s/ingress.yaml
```

Add host mapping:

```
$(minikube ip) metrics.local
```

Check:

```
curl http://metrics.local/metrics
curl http://metrics.local/analytics
```

### 5) HPA

```
kubectl apply -f k8s/hpa.yaml
```

Verify:

```
kubectl get hpa
```

## Prometheus + Grafana (Helm)

### 1) Install Prometheus + Alertmanager rules

```
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update
helm install prometheus prometheus-community/prometheus -f k8s/prometheus-values.yaml
```

Prometheus will auto-scrape pods annotated with `prometheus.io/scrape: "true"`.

### 2) Install Grafana

```
helm repo add grafana https://grafana.github.io/helm-charts
helm repo update
helm install grafana grafana/grafana \
  --set sidecar.dashboards.enabled=true \
  --set sidecar.dashboards.label=grafana_dashboard
```

Get the admin password:

```
kubectl get secret --namespace default grafana -o jsonpath="{.data.admin-password}" | base64 --decode ; echo
```

Port-forward Grafana:

```
kubectl port-forward svc/grafana 3000:80
```

Apply dashboards:

```
kubectl apply -f k8s/grafana-dashboards.yaml
```

### 3) Prometheus metrics in this service

- RPS: `rate(ingest_total[1m])`
- Latency: `ingest_processing_seconds_bucket` (histogram)
- Anomaly rate (RPS): `rate(analytics_anomaly_rps_total[5m]) / rate(ingest_total[5m])`

### 4) Alertmanager

Rule is in `k8s/prometheus-values.yaml`:
- Alert `HighAnomalyRate` fires if anomalies > 5 per minute for 1 minute.

Port-forward Alertmanager:

```
kubectl port-forward svc/prometheus-alertmanager 9093:9093
```
