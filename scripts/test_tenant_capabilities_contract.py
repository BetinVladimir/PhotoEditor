"""Authored advisory capability DTO projection cases; not executed in coding."""
import importlib.util
import json
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]
PATH = '/api/v1/tenants/{t}/capabilities'

class TenantCapabilitiesContract(unittest.TestCase):
    def test_current_read_has_no_body_or_mutation_headers(self):
        selected = []
        for file in ['docs/implementation-v2/contracts/endpoints.json', 'packages/contracts/endpoints.json', 'packages/contracts/v2-endpoints.json']:
            selected = [r for r in json.loads((ROOT/file).read_text())['endpoints'] if r['path']==PATH and r['method']=='GET']
            self.assertEqual(len(selected), 1)
            self.assertEqual(selected[0]['response_schema'], 'TenantCapabilities')
            self.assertEqual(selected[0]['role'], 'viewer')
            self.assertFalse(selected[0]['idempotency'])
            self.assertIsNone(selected[0]['request_schema'])
        spec = importlib.util.spec_from_file_location('cap_sync', ROOT/'scripts/sync-v2-openapi.py')
        module = importlib.util.module_from_spec(spec); spec.loader.exec_module(module)
        for doc in [json.loads((ROOT/'docs/implementation-v2/contracts/openapi.json').read_text()), module.synchronize({'paths': {}}, selected)]:
            op = doc['paths'][PATH]['get']
            self.assertNotIn('requestBody', op)
            self.assertEqual([(v['in'],v['name']) for v in op['parameters']], [('path','t')])
            self.assertFalse(op['x-web-mutation-csrf'])
            self.assertEqual(op['responses']['200']['content']['application/json']['schema']['$ref'], 'schemas.json#/$defs/TenantCapabilities')

    def test_nullable_metadata_and_readiness_are_explicit(self):
        defs = json.loads((ROOT/'docs/implementation-v2/contracts/schemas.json').read_text())['$defs']
        operation = defs['TenantCapabilityOperation']
        self.assertFalse(operation['additionalProperties'])
        self.assertEqual(set(operation['required']), set(operation['properties']))
        for name in ['model_id','reason']:
            self.assertIn({'type':'null'}, operation['properties'][name]['anyOf'])
        ready = operation['allOf'][0]['then']['properties']
        self.assertEqual(ready['available'], {'const':True})
        self.assertEqual(ready['reason'], {'type':'null'})
        self.assertEqual(ready['available_backends']['minItems'], 1)
