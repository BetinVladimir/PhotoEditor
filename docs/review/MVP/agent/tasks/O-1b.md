---
id: O-1b
title: deploy/vpn - WireGuard hub/worker templates and nftables policy
area: ops
size: S
depends: [O-1a]
edit:
  - deploy/vpn/wg-hub.conf.example (new)
  - deploy/vpn/wg-worker.conf.example (new)
  - deploy/vpn/worker-firewall.nft (new)
  - deploy/vpn/README.md (new)
context:
  - docs/ops/network-topology.md
verify:
  - grep -q "PersistentKeepalive = 25" deploy/vpn/wg-worker.conf.example
  - grep -q "5671" deploy/vpn/worker-firewall.nft
---
## Spec
Templates only; no real keys (`<HUB_PRIVATE_KEY>`, `<WORKER_PUBLIC_KEY>` style placeholders).
- `wg-hub.conf.example`: `[Interface]` Address `10.77.0.1/24`, ListenPort 51820, MTU 1380, PostUp/PostDown enabling forwarding are NOT needed (hub terminates services locally) - omit; two example `[Peer]` blocks `10.77.0.10/32`, `10.77.0.11/32`.
- `wg-worker.conf.example`: `[Interface]` Address `10.77.0.10/32`, MTU 1380; `[Peer]` hub with `AllowedIPs = 10.77.0.1/32`, `Endpoint = vpn.example.org:51820`, `PersistentKeepalive = 25`.
- `worker-firewall.nft`: nftables `inet filter` table, default policy drop for `input` and `forward`, allow established/related, loopback, SSH from `192.0.2.0/24` (documentation range, comment that it must be replaced), `output` chain policy accept restricted to `10.77.0.1` tcp dports `{5671, 8443, 5432}` plus DNS/NTP to `10.77.0.1` udp `{53, 123}`; comment each rule.
- `README.md`: 15-25 lines: how to create keys (`wg genkey | tee ... | wg pubkey`), install the files, rotate keys every 90 days, and a pointer to `docs/ops/network-topology.md`.
