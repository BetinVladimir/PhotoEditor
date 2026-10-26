---
id: O-1a
title: docs/ops/network-topology.md (servers <-> GPU workers over VPN)
area: ops
size: S
depends: []
edit:
  - docs/ops/network-topology.md (new)
context:
  - docs/review/MVP/05-deploy-and-ops.md:18-32
verify:
  - python3 -c "t=open('docs/ops/network-topology.md').read(); [t.index(h) for h in ('## Topology','## Addressing','## Firewall rules','## Failure modes','## Certificates and names','## Time synchronisation')]"
---
## Goal
The authoritative network document that ops and the worker installers follow. Write it in English, concise, no hostnames/IPs of real servers (use the documented example ranges).

## Required sections (exact headings)
- `## Topology`: a mermaid `flowchart LR` with: Internet -> Caddy (nodes A/B) -> API/gateway/portal/photographer -> PostgreSQL, RabbitMQ, MinIO/S3; WireGuard hub; GPU workers (CUDA hosts, Apple Silicon hosts) connecting out through the hub. State the rule: *workers initiate every connection (egress only); nothing on the server side connects to a worker*.
- `## Addressing`: WireGuard subnet `10.77.0.0/24`, hub `10.77.0.1`, workers `/32` from `10.77.0.10`, `PersistentKeepalive = 25`, MTU 1380, key rotation every 90 days.
- `## Firewall rules`: table of allowed flows for worker peers: `10.77.0.1:5671` (AMQPS), `10.77.0.1:8443` (object gateway, mTLS), `10.77.0.1:5432` (PostgreSQL, TLS `verify-full`, only until the control-plane API replaces direct SQL); inbound to workers only SSH from the admin network.
- `## Certificates and names`: one internal CA; server certs for RabbitMQ/PostgreSQL/gateway; client cert per worker with `CN=<worker_id>`; DNS names `queue.photo.internal`, `gateway.photo.internal`, `db.photo.internal` must appear in the SANs.
- `## Time synchronisation`: chrony/NTP on every node; the worker refuses to start when its clock differs by more than 2 s from the database (`WORKER_MAX_CLOCK_SKEW_SECONDS`).
- `## Failure modes`: table `outage length | expected behaviour`: < 20 s task continues; 20-60 s task continues, new tasks paused (capacity heartbeat degraded); longer than the lease (default see capacity policy) task is lost and re-executed idempotently; after recovery a spooled result is delivered at worker start.
