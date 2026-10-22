"""Authored contract projection scenarios; deliberately not executed."""
import json
from pathlib import Path
import unittest
ROOT = Path(__file__).resolve().parents[1]
PATH = '/api/v1/tenants/{t}/collections/{id}/bulk-operations/abandon'

class CollectionBulkContract(unittest.TestCase):
    def test_all_packaged_registries_keep_original_key_combined_request(self):
        for name in ('docs/implementation-v2/contracts/endpoints.json','packages/contracts/endpoints.json','packages/contracts/v2-endpoints.json'):
            rows=json.loads((ROOT/name).read_text())['endpoints']
            matches=[r for r in rows if r['method']=='POST' and r['path']==PATH]
            self.assertEqual(len(matches),1)
            row=matches[0]
            self.assertEqual(row['role'],'editor')
            self.assertTrue(row['idempotency'])
            self.assertEqual(row['request_schema'],'CollectionBulkAbandonRequest')
    def test_abandon_never_projects_accepted_receipt_as_a_fake_snapshot(self):
        api=json.loads((ROOT/'docs/implementation-v2/contracts/openapi.json').read_text())['paths'][PATH]['post']
        self.assertEqual(api['responses']['200']['content']['application/json']['schema']['$ref'],'schemas.json#/$defs/CollectionBulkAbandoned')
        self.assertEqual(api['responses']['202']['content']['application/json']['schema']['$ref'],'schemas.json#/$defs/CollectionBulkAcceptedResolution')
        defs=json.loads((ROOT/'docs/implementation-v2/contracts/schemas.json').read_text())['$defs']
        resolved=defs['CollectionBulkAcceptedResolution']
        self.assertEqual(set(resolved['required']),{'status','snapshot','ticket'})
        self.assertEqual(resolved['properties']['status'],{'const':'accepted'})
        self.assertEqual(len(defs['CollectionBulkAbandonRequest']['oneOf']),9)
        for branch in defs['CollectionBulkAbandonRequest']['oneOf']:
            self.assertIn('selection_key',branch['required'])
            self.assertFalse(branch['additionalProperties'])


    def test_whole_five_current_routes_and_fresh_projection(self):
        import importlib.util
        directory=ROOT/'docs/implementation-v2/contracts'
        base='/api/v1/tenants/{t}'
        wanted={('POST',base+'/collections/{id}/selection-snapshots'):('viewer',201,'CollectionBulkSnapshot'),('POST',base+'/collections/{id}/bulk-operations'):('editor',202,'CollectionBulkTicket'),('POST',PATH):('editor',200,'CollectionBulkAbandonResolution'),('GET',base+'/bulk-operations/{id}'):('viewer',200,'CollectionBulkStatus'),('POST',base+'/bulk-operations/{id}/cancel'):('editor',200,'CollectionBulkStatus')}
        selected=[]
        for name in ('docs/implementation-v2/contracts/endpoints.json','packages/contracts/endpoints.json','packages/contracts/v2-endpoints.json'):
            rows=json.loads((ROOT/name).read_text())['endpoints']
            current=[r for r in rows if (r['method'],r['path']) in wanted]
            self.assertEqual(len(current),5)
            for row in current:
                role,status,response=wanted[row['method'],row['path']]
                self.assertEqual((row['role'],row['success_status'],row['response_schema']),(role,status,response))
            selected=current
        spec=importlib.util.spec_from_file_location('bulk_sync',ROOT/'scripts/sync-v2-openapi.py');module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
        for paths in [json.loads((directory/'openapi.json').read_text())['paths'],module.synchronize({'paths':{}},selected)['paths']]:
            for row in selected:
                op=paths[row['path']][row['method'].lower()]
                self.assertEqual(op['x-role'],row['role'])
                headers={p['name'] for p in op['parameters'] if p['in']=='header'}
                self.assertEqual(headers,{'Idempotency-Key'} if row['idempotency'] else set())
                if row['request_schema'] is None:self.assertNotIn('requestBody',op)
                else:self.assertEqual(op['requestBody']['content']['application/json']['schema']['$ref'],'schemas.json#/$defs/'+row['request_schema'])
                if row['method']=='GET':
                    query={p['name']:p['schema'] for p in op['parameters'] if p['in']=='query'}
                    self.assertEqual(query['limit']['maximum'],100);self.assertEqual(query['limit']['default'],50)
        defs=json.loads((directory/'schemas.json').read_text())['$defs']
        self.assertEqual(len(defs['CollectionBulkStartRequest']['oneOf']),9)
        report=defs['CollectionBulkStatus'];self.assertFalse(report['additionalProperties'])
        page=report['properties']['results'];self.assertEqual(set(page['required']),{'items','next_cursor'})
        item=page['properties']['items']['items'];self.assertNotIn('error_code',item['required']);self.assertIn('result',item['required']);self.assertEqual(item['properties']['result'],{})
