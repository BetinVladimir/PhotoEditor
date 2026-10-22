"""Authored source contracts; execution belongs to the testing phase."""
import json
import unittest
from pathlib import Path
ROOT = Path(__file__).resolve().parents[1]
class OpticalEndpointAuthority(unittest.TestCase):
    def test_result_read_roles_match_editor_bound_domain_projection(self):
        fragments = ('/perspective-proposals/', '/panorama-proposals/', '/focus-stack-proposals/')
        for relative in ('docs/implementation-v2/contracts/endpoints.json', 'packages/contracts/v2-endpoints.json', 'packages/contracts/endpoints.json'):
            rows = json.loads((ROOT / relative).read_text())['endpoints']
            reads = [r for r in rows if r['method'] == 'GET' and any(f in r['path'] for f in fragments)]
            self.assertEqual(len(reads), 9, relative)
            self.assertTrue(all(r['role'] == 'editor' for r in reads), relative)
            api = json.loads((ROOT / 'docs/implementation-v2/contracts/openapi.json').read_text())['paths']
            for r in reads:
                self.assertEqual(api[r['path']]['get']['x-role'], 'editor')
                self.assertEqual(api[r['path']]['get']['security'], [{'userBearer': []}])
