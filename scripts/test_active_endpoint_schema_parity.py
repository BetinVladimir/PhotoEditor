"""Authored schema/path parity for three actual mounted DTOs."""
import importlib.util
import json
from pathlib import Path
import re
import unittest

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('sync_active_openapi', Path(__file__).with_name('sync-v2-openapi.py'))
projection = importlib.util.module_from_spec(spec)
spec.loader.exec_module(projection)


class ActiveSchemaParity(unittest.TestCase):
    def test_session_analysis_revision_matches_actual_safe_integer_parser(self):
        schemas = json.loads((ROOT/'docs/implementation-v2/contracts/schemas.json').read_text())['$defs']
        self.assertEqual(schemas['AnalysisCreate']['properties']['expected_revision'], {'$ref':'#/$defs/PositiveSafeIntegerRevision'})
        pattern = schemas['PositiveSafeIntegerRevision']['pattern']
        for value in ('1', '9007199254740990', '9007199254740991'):
            self.assertIsNotNone(re.fullmatch(pattern, value))
        for value in ('0', '01', '-1', '1.0', '9007199254740992', '9223372036854775807'):
            self.assertIsNone(re.fullmatch(pattern, value))
        self.assertEqual(schemas['AnalysisCreate']['properties']['selection_snapshot_id']['not']['const'], '00000000-0000-0000-0000-000000000000')

    def test_three_routes_remain_complete_when_openapi_is_rebuilt_from_empty(self):
        paths = ('/api/v1/tenants/{t}/shoot-sessions/{id}/analysis-capabilities',
                 '/api/v1/tenants/{t}/collections/{id}/shares/{share}/usage',
                 '/api/v1/tenants/{t}/collections/{id}/publication')
        rows = [r for r in json.loads((ROOT/'docs/implementation-v2/contracts/endpoints.json').read_text())['endpoints'] if r['path'] in paths]
        generated = projection.synchronize({'paths':{}}, rows)
        saved = json.loads((ROOT/'docs/implementation-v2/contracts/openapi.json').read_text())
        for document in (generated, saved):
            for path in paths:
                method = 'get' if path.endswith('/usage') else 'post'
                op = document['paths'][path][method]
                parameters = [p for p in op['parameters'] if p['in']=='path']
                self.assertEqual({p['name'] for p in parameters}, set(re.findall(r'{([^}]+)}',path)))
                self.assertTrue(all(p['required'] and p['schema']['format']=='uuid' for p in parameters))
                self.assertEqual(op['security'], [{'userBearer':[]}])
                self.assertEqual(op['x-web-mutation-csrf'], method!='get')
                self.assertFalse(op['x-idempotency'])
                self.assertFalse(any(p['in']=='header' and p['name'] in ('If-Match','Idempotency-Key') for p in op['parameters']))
                if path.endswith('/analysis-capabilities'):
                    self.assertTrue(op['x-read-only-post'])
                    self.assertFalse(op['x-query-parameters-allowed'])
        self.assertEqual(projection.synchronize(generated, rows), generated)


if __name__ == '__main__':
    unittest.main()
