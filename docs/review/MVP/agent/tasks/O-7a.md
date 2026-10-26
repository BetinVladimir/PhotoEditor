---
id: O-7a
title: Scanner for private identifiers committed to the repository
area: ops
size: S
depends: []
edit:
  - scripts/scan-private-ids.py (new)
  - tests/deploy/test_scan_private_ids.py (new)
context: []
verify:
  - python3 -m pytest tests/deploy/test_scan_private_ids.py -q -p no:cacheprovider
---
## Goal
Plan item O-7: find (not fix) server IPs, Cloudflare account/zone ids and SMTP/forwarding addresses in tracked files, so a human can decide what to remove.

## Spec (`scan-private-ids.py`, stdlib only)
- `python3 scripts/scan-private-ids.py [--root DIR] [--allow FILE]`: walk `git ls-files` output when `--root` is a git repo, else all text files (skip files > 1 MiB and binary files).
- Findings: (1) IPv4 addresses that are not private/loopback/documentation/link-local (`ipaddress` module), (2) 32-hex strings within 40 characters after `account_id`, `zone_id`, `account`, `zone` (case-insensitive), (3) e-mail addresses whose domain is not `example.*`, `localhost`, or listed in the allow file.
- Output one line per finding `path:line: kind: value-masked` (mask the middle of the value: first 3 and last 2 characters visible), summary count, exit 1 if any finding survives the allow file (lines `path-glob` or `kind:value` ), else 0.
- Test creates a temporary directory with sample files (including negative samples: `10.0.0.1`, `192.0.2.5`, `user@example.com`, a 32-hex string without a key word before it) and asserts the findings and the allow-file behaviour.

## Do not
- Do not print unmasked values. Do not modify any scanned file.
