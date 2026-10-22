"""Authored contract boundaries; no execution or physical qualification implied."""
import importlib.util
import json
from pathlib import Path
import re
import unittest

ROOT = Path(__file__).resolve().parents[1]
BASE = '/api/v1/tenants/{t}/delivery-packages'
SIGNING = '/api/v1/tenants/{t}/projects/{project}/delivery-signing-policies'


class DeliveryEndpointDTOContract(unittest.TestCase):
    def setUp(self):
        directory = ROOT / 'docs/implementation-v2/contracts'
        self.defs = json.loads((directory / 'schemas.json').read_text())['$defs']
        self.rows = json.loads((directory / 'endpoints.json').read_text())['endpoints']
        self.rows = [row for row in self.rows if '/delivery-packages' in row['path'] or row['path'] == SIGNING]
        self.paths = json.loads((directory / 'openapi.json').read_text())['paths']

    def test_exact_package_projection_and_string_cas(self):
        self.assertEqual(len(self.rows), 10)
        for row in self.rows:
            op = self.paths[row['path']][row['method'].lower()]
            ref = op['responses'][str(row['success_status'])]['content']['application/json']['schema']['$ref']
            self.assertEqual(ref, 'schemas.json#/$defs/' + row['response_schema'])
            if row['handler'] in ('GetDelivery', 'GetDeliveryByRun', 'GetDeliveryByWorkflow', 'PublishDelivery'):
                self.assertEqual(row['response_schema'], 'DeliveryPackageResource')
            if row['method'] == 'GET':
                self.assertNotIn('requestBody', op)
            else:
                self.assertEqual(op['requestBody']['content']['application/json']['schema']['$ref'], 'schemas.json#/$defs/' + row['request_schema'])
        envelope = self.defs['DeliveryPackageResource']
        self.assertFalse(envelope['additionalProperties'])
        self.assertEqual(set(envelope['required']), {'id', 'revision', 'document', 'data'})
        self.assertEqual(envelope['properties']['data']['$ref'], '#/$defs/RowDeliveryPackages')
        self.assertEqual(self.defs['RowDeliveryPackages']['properties']['name']['maxLength'], 200)
        inspection = next(row for row in self.rows if row['handler'] == 'InspectDelivery')
        self.assertEqual(inspection['role'], 'editor')
        self.assertEqual(self.paths[inspection['path']]['get']['x-role'], 'editor')

    def test_decimal_boundaries_never_coerce_revision_to_number(self):
        revision = self.defs['DeliveryRevision']['pattern']
        publish = self.defs['DeliveryPublishRequest']['properties']['expected_revision']['pattern']
        for value in ['1', '9', '9007199254740990', '9007199254740991']:
            self.assertIsNotNone(re.fullmatch(revision, value), value)
        for value in ['', '0', '01', '+1', '1.0', '9007199254740992', '9223372036854775807']:
            self.assertIsNone(re.fullmatch(revision, value), value)
        self.assertIsNotNone(re.fullmatch(publish, '9007199254740990'))
        self.assertIsNone(re.fullmatch(publish, '9007199254740991'))
        self.assertEqual(self.defs['DeliveryInspection']['properties']['ready_count']['minimum'], 1)
        self.assertEqual(self.defs['DeliveryInspection']['properties']['slot_count']['minimum'], 1)

    def test_future_projection_preserves_query_only_reads_and_intent_authority(self):
        spec = importlib.util.spec_from_file_location('delivery_scope_sync', ROOT / 'scripts/sync-v2-openapi.py')
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        paths = module.synchronize({'paths': {}}, self.rows)['paths']
        for row in self.rows:
            op = paths[row['path']][row['method'].lower()]
            if row['method'] == 'GET': self.assertNotIn('requestBody', op)
            else:
                key = next(p for p in op['parameters'] if p['name'] == 'Idempotency-Key')
                self.assertTrue(key['required'])
                self.assertEqual(key['schema']['maxLength'], 128)
        items = {p['name']: p for p in paths[BASE + '/{id}/items']['get']['parameters']}
        self.assertEqual(items['revision']['schema']['$ref'], 'schemas.json#/$defs/DeliveryRevision')
        self.assertEqual(items['limit']['schema']['maximum'], 200)
        self.assertEqual(items['after_ordinal']['schema']['maximum'], 4999)
        mime = next(p for p in paths[SIGNING]['get']['parameters'] if p['name'] == 'mime')
        self.assertTrue(mime['explode'])
        self.assertTrue(mime['schema']['uniqueItems'])
        self.assertEqual(mime['schema']['items']['enum'], ['image/jpeg', 'image/png'])

    def test_manifest_and_slot_are_closed_nonnullable_variants(self):
        manifest, slot = self.defs['DeliveryDownloadIntentRequest']['oneOf']
        self.assertEqual(set(manifest['properties']), {'kind'})
        self.assertFalse(manifest['additionalProperties'])
        self.assertFalse(slot['additionalProperties'])
        self.assertEqual(set(slot['required']), {'kind', 'ordinal', 'preset_id'})
        self.assertEqual(slot['properties']['ordinal']['type'], 'integer')
        self.assertEqual(slot['properties']['preset_id']['not']['const'], '00000000-0000-0000-0000-000000000000')
        token = self.defs['DeliveryDownloadIntent']['properties']['intent_token']['pattern']
        self.assertIsNotNone(re.fullmatch(token, 'a' * 42 + 'A'))
        self.assertIsNone(re.fullmatch(token, 'a' * 42 + 'b'))


if __name__ == '__main__':
    unittest.main()
