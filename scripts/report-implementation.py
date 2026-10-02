#!/usr/bin/env python3
"""Report actual HTTP coverage without treating schemas as implementations."""
from pathlib import Path
import json
import re
ROOT = Path(__file__).resolve().parents[1]
contract = json.loads((ROOT/'docs/implementation/contracts/endpoints.json').read_text())['endpoints']
routes = set()
for path in (ROOT/'backend/internal/httpapi').glob('*.go'):
    if path.name.endswith('_test.go'):
        continue
    for method, route in re.findall(r'\br\.(Get|Post|Put|Patch|Delete)\("([^\"]+)"', path.read_text()):
        if route.startswith('/api/'):
            routes.add((method.upper(), route))
known = {(e['method'], e['path']) for e in contract}
if routes-known:
    raise SystemExit(f'Undocumented routes: {sorted(routes-known)}')
report = {'format_version':1, 'scope':'HTTP handlers present; this does not certify acceptance or completeness',
          'implemented_routes':len(routes), 'required_routes':len(known),
          'endpoints':[{'method':e['method'], 'path':e['path'], 'handler_present':(e['method'],e['path']) in routes} for e in contract]}
(ROOT/'docs/implementation-progress/coverage.json').write_text(json.dumps(report, ensure_ascii=False, indent=2)+'\n')
print(f'HTTP handlers: {len(routes)}/{len(known)}; release acceptance remains incomplete')
