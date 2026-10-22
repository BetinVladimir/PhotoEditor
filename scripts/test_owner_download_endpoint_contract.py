"""Authored owner Download4 source-contract scenarios; not executed in coding phase."""
import json
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]
BASE = '/api/v1/tenants/{t}/download-jobs'
ROWS = [('POST', BASE, 'OwnerDownloadRequest', 'OwnerDownloadAccepted', 202, True),
        ('GET', BASE+'/{id}', None, 'OwnerDownloadStatus', 200, False),
        ('POST', BASE+'/{id}/cancel', None, 'OwnerDownloadStatus', 200, False),
        ('POST', BASE+'/{id}/download-intent', 'OwnerDownloadIntentRequest', 'OwnerDownloadIntent', 201, True)]

class OwnerDownloadContracts(unittest.TestCase):
    def test_exact_mirrors_and_manual_openapi(self):
        api = json.loads((ROOT/'docs/implementation-v2/contracts/openapi.json').read_text())
        for name in ('docs/implementation-v2/contracts/endpoints.json', 'packages/contracts/v2-endpoints.json', 'packages/contracts/endpoints.json'):
            rows=json.loads((ROOT/name).read_text())['endpoints']
            for method,path,request,response,status,idem in ROWS:
                with self.subTest(registry=name,path=path,method=method):
                    found=[r for r in rows if r['method']==method and r['path']==path]
                    self.assertEqual(len(found),1)
                    row=found[0]
                    self.assertEqual((row['request_schema'],row['response_schema'],row['success_status'],row['role'],row['idempotency']),(request,response,status,'viewer',idem))
                    op=api['paths'][path][method.lower()]
                    self.assertEqual(op['x-role'],'viewer')
                    self.assertEqual(op['responses'][str(status)]['content']['application/json']['schema']['$ref'],'schemas.json#/$defs/'+response)
                    headers=[p['name'] for p in op['parameters'] if p['in']=='header']
                    self.assertEqual('Idempotency-Key' in headers,idem)
                    self.assertEqual('requestBody' in op,request is not None)

    def test_status_optional_fields_and_exact_required_shape(self):
        defs=json.loads((ROOT/'docs/implementation-v2/contracts/schemas.json').read_text())['$defs']
        status=defs['OwnerDownloadStatus']
        self.assertNotIn('expires_at',status['required'])
        self.assertNotIn('error',status['required'])
        self.assertFalse(status['additionalProperties'])
        self.assertEqual(defs['OwnerDownloadIntentRequest']['properties']['volume']['maximum'],5000)
