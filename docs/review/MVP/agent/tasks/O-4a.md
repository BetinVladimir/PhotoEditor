---
id: O-4a
title: Prometheus scrape config and alert rules
area: ops
size: S
depends: [B-2a]
edit:
  - deploy/observability/prometheus.yml (new)
  - deploy/observability/alerts.yml (new)
context:
  - docs/review/MVP/05-deploy-and-ops.md:56-62
verify:
  - python3 -c "import yaml; yaml.safe_load(open('deploy/observability/prometheus.yml')); d=yaml.safe_load(open('deploy/observability/alerts.yml')); assert len(d['groups'][0]['rules'])>=8"
---
## Spec
`prometheus.yml`: `global.scrape_interval: 15s`, `rule_files: [alerts.yml]`, scrape jobs `photo-api` (target `api:9102`, path `/metrics`; this is the `METRICS_LISTEN=:9102` private listener), `rabbitmq` (`rabbitmq:15692`), `postgres` (`postgres-exporter:9187`), `node` (`node-exporter:9100`).
`alerts.yml`: one group `photo`, rules (each with `for`, `labels.severity`, `annotations.summary`):
1. `ApiDown`: `up{job="photo-api"} == 0` for 2m, critical.
2. `ApiHighErrorRate`: `sum(rate(photo_http_requests_total{status=~"5.."}[5m])) / sum(rate(photo_http_requests_total[5m])) > 0.05` for 10m, warning.
3. `ApiSlow`: `histogram_quantile(0.95, sum by (le) (rate(photo_http_request_duration_seconds_bucket[5m]))) > 1` for 10m, warning.
4. `OutboxLag`: `photo_outbox_oldest_age_seconds > 60` for 5m, critical (metric to be added by a later card; keep).
5. `QueueGrowing`: `deriv(rabbitmq_queue_messages_ready{queue=~"q\\.worker\\..*"}[10m]) > 0 and rabbitmq_queue_messages_ready > 20` for 10m, warning.
6. `WorkerSilent`: `time() - photo_worker_last_heartbeat_timestamp_seconds > 60` for 2m, critical.
7. `SchedulerNoLeader`: `sum(photo_scheduler_is_leader) == 0` for 2m, critical.
8. `DiskScratchHigh`: `1 - node_filesystem_avail_bytes{mountpoint="/scratch"} / node_filesystem_size_bytes{mountpoint="/scratch"} > 0.85` for 10m, warning.
9. `CertificateExpiry`: `(probe_ssl_earliest_cert_expiry - time()) / 86400 < 21` for 1h, warning.
10. `PostgresReplicationLag`: `pg_replication_lag_seconds > 30` for 5m, warning.
