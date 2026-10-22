"""Authored source projection scenarios; no execution during the coding phase."""
import importlib.util
import json
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]
BASE = '/api/v1/tenants/{t}/'
ROWS = {
    BASE+'masks': ('MaskCreate', 'MaskAccepted', 202),
    BASE+'generations': ('GenerateRequest', 'RunAccepted', 202),
    BASE+'generations/abandon': ('GenerateRequest', 'GenerationAbandonment', 200),
    BASE+'previews': ('PreviewRequest', 'RunAccepted', 202),
    BASE+'overrides': ('OverrideCreate', 'Override', 201),
}

class InteractiveContracts(unittest.TestCase):
    def test_exact_five_projections_and_preserved_abandonment_union(self):
        selected = []
        for file in ['docs/implementation-v2/contracts/endpoints.json',
                     'packages/contracts/endpoints.json', 'packages/contracts/v2-endpoints.json']:
            selected = [row for row in json.loads((ROOT/file).read_text())['endpoints']
                        if row['method']=='POST' and row['path'] in ROWS]
            self.assertEqual(len(selected), 5)
            for row in selected:
                self.assertEqual((row['request_schema'], row['response_schema'], row['success_status']), ROWS[row['path']])
                self.assertEqual(row['role'], 'editor')
                self.assertTrue(row['idempotency'])
        spec = importlib.util.spec_from_file_location('interactive_sync', ROOT/'scripts/sync-v2-openapi.py')
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        manual = json.loads((ROOT/'docs/implementation-v2/contracts/openapi.json').read_text())
        for doc in [manual, module.synchronize({'paths': {}}, selected)]:
            for path, (request, response, status) in ROWS.items():
                op = doc['paths'][path]['post']
                self.assertEqual(op['x-role'], 'editor')
                self.assertTrue(op['x-web-mutation-csrf'])
                self.assertEqual(op['requestBody']['content']['application/json']['schema']['$ref'], 'schemas.json#/$defs/'+request)
                self.assertEqual(op['responses'][str(status)]['content']['application/json']['schema']['$ref'], 'schemas.json#/$defs/'+response)
                headers = {v['name'] for v in op['parameters'] if v['in']=='header'}
                self.assertEqual(headers, {'Idempotency-Key', 'Generation-Replay-Only'} if path.endswith('/generations') else {'Idempotency-Key'})
                self.assertFalse(any(v['in']=='query' for v in op['parameters']))
            self.assertEqual(doc['paths'][BASE+'generations/abandon']['post']['responses']['202']['content']['application/json']['schema']['$ref'], 'schemas.json#/$defs/RunAccepted')

    def test_immutable_admission_receipts_are_closed_and_source_bound(self):
        defs = json.loads((ROOT/'docs/implementation-v2/contracts/schemas.json').read_text())['$defs']
        for name, required in [('MaskAccepted', {'run_id','status','source_sha256'}), ('RunAccepted', {'run_id','status'})]:
            self.assertEqual(set(defs[name]['required']), required)
            self.assertFalse(defs[name]['additionalProperties'])
            self.assertEqual(defs[name]['properties']['status']['const'], 'queued')
        self.assertEqual(len(defs['GenerationAbandonment']['oneOf']), 2)
        self.assertEqual(defs['PreviewRequest']['properties']['generation']['const'], False)
