---
id: W-6b
title: RabbitMQ over TLS (5671) - config file and compose wiring
area: deploy
size: S
depends: []
edit:
  - deploy/rabbitmq/rabbitmq.conf (new)
  - deploy/compose.yaml
context:
  - deploy/compose.yaml:76-92
  - deploy/compose.yaml:365-372
verify:
  - python3 -c "import re,sys; t=open('deploy/rabbitmq/rabbitmq.conf').read(); assert 'listeners.ssl.default = 5671' in t and 'listeners.tcp = none' in t and 'fail_if_no_peer_cert = true' in t"
  - python3 -c "import yaml; d=yaml.safe_load(open('deploy/compose.yaml')); r=d['services']['rabbitmq']; assert any('rabbitmq.conf' in str(v) for v in r['volumes'])"
---
## Goal
Worker nodes reach the broker over the VPN: the broker must speak only TLS with mandatory client certificates issued by the same internal CA that already signs the gateway/worker certificates.

## Spec
1. Create `deploy/rabbitmq/rabbitmq.conf`:
```
listeners.tcp = none
listeners.ssl.default = 5671
ssl_options.cacertfile = /run/photo/tls/ca.crt
ssl_options.certfile   = /run/photo/tls/server.crt
ssl_options.keyfile    = /run/photo/tls/server.key
ssl_options.verify = verify_peer
ssl_options.fail_if_no_peer_cert = true
ssl_options.versions.1 = tlsv1.3
ssl_options.versions.2 = tlsv1.2
auth_mechanisms.1 = PLAIN
management.tcp.port = 15672
management.tcp.ip = 127.0.0.1
```
2. In the `rabbitmq` service of `deploy/compose.yaml` add to `volumes` (keep the existing `rabbit-data` entry):
   - `./rabbitmq/rabbitmq.conf:/etc/rabbitmq/rabbitmq.conf:ro`
   - `${PHOTO_SECRETS_DIR:?required}/rabbitmq/ca.crt:/run/photo/tls/ca.crt:ro`, `.../rabbitmq/server.crt:/run/photo/tls/server.crt:ro`, `.../rabbitmq/server.key:/run/photo/tls/server.key:ro`
   and `expose: ["5671"]`. Change the healthcheck to `["CMD", "rabbitmq-diagnostics", "-q", "check_port_connectivity"]`.
3. Do NOT publish port 5671 to the host here (VPN exposure is configured per environment in the ops docs).
4. Services that connect to the broker use `AMQP_URL` from the environment; do not edit them here.
