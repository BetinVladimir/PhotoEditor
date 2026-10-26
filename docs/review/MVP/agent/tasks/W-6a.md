---
id: W-6a
title: AMQP connection parameters for VPN (timeouts, keepalive, client TLS)
area: worker
size: S
depends: []
edit:
  - workers/photo_worker/runtime/transport.py
  - workers/tests/test_transport_parameters.py (new)
context:
  - workers/photo_worker/runtime/transport.py::Transport.consume
verify:
  - cd workers && python -m pytest tests/test_transport_parameters.py tests/test_transport_pause.py -q -p no:cacheprovider
---
## Goal
Move the pika parameter construction out of `consume` into a pure, testable function and make it VPN-friendly.

## Spec
Add to `transport.py`:
```python
def build_parameters(url: str, env=None) -> pika.URLParameters:
```
(`env` defaults to `os.environ`; add the imports `os`, `socket`, `ssl`.)
- `p = pika.URLParameters(url)`; `p.heartbeat = 30`; `p.socket_timeout = int(env.get('WORKER_AMQP_SOCKET_TIMEOUT', 10))`; `p.stack_timeout = int(env.get('WORKER_AMQP_STACK_TIMEOUT', 20))`; `p.blocked_connection_timeout = 10`; `p.connection_attempts = 1`. Clamp each integer to `[2, 60]`.
- Keepalive: build `opts = {}` and include `TCP_KEEPIDLE: 15`, `TCP_KEEPINTVL: 5`, `TCP_KEEPCNT: 3` only for names that exist in `socket` (use `hasattr(socket, name)`); `p.tcp_options = opts or None`.
- TLS: only when `urlparse(url).scheme == 'amqps'`. Read `WORKER_AMQP_CA_FILE` (required for amqps unless unset => system CAs), optional `WORKER_AMQP_CERT_FILE` + `WORKER_AMQP_KEY_FILE` (both or neither, else `ValueError('Invalid AMQP client certificate configuration')`). Context: `ssl.create_default_context(cafile=ca)`; `ctx.minimum_version = ssl.TLSVersion.TLSv1_2`; `ctx.load_cert_chain(cert, key)` when given; `p.ssl_options = pika.SSLOptions(ctx, urlparse(url).hostname)`.
In `consume` replace the inline parameter lines (everything from `pika.URLParameters(self._url)` to `connection_attempts = 1`) by `parameters = build_parameters(self._url)`.

## Test
Pure function tests: defaults; env override and clamping; keepalive options contain only existing socket constants (monkeypatch `socket` attribute removal with `monkeypatch.delattr(socket, 'TCP_KEEPIDLE', raising=False)`); `amqp://` has no ssl_options; `amqps://` with a CA file created by `ssl.create_default_context` is impractical, so monkeypatch `ssl.create_default_context` to return a `MagicMock` and assert `load_cert_chain` is called only when both cert and key are set, and that exactly one of them raises ValueError.
