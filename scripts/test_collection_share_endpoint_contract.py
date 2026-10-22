"""Authored source-contract scenarios, never executed during coding phase."""
import importlib.util
import json
from pathlib import Path
import unittest
ROOT=Path(__file__).resolve().parents[1]
BASE='/api/v1/tenants/{t}/collections/{id}/shares'
KEYS={('GET',BASE):('CollectionSharePage',200),('POST',BASE):('CollectionShareCreated',201),('PATCH',BASE+'/{share}'):('CollectionShare',200),('POST',BASE+'/{share}/revoke'):('CollectionShare',200),('GET',BASE+'/{share}/usage'):('CollectionShareUsage',200),('POST',BASE+'/abandon'):('ShareCreationResolution',200)}
class CollectionShareEndpointContract(unittest.TestCase):
 def test_actual_six_routes_have_consistent_typed_registry_and_fresh_projection(self):
  directory=ROOT/'docs/implementation-v2/contracts';selected=[]
  for name in ['docs/implementation-v2/contracts/endpoints.json','packages/contracts/endpoints.json','packages/contracts/v2-endpoints.json']:
   rows=json.loads((ROOT/name).read_text())['endpoints'];selected=[r for r in rows if (r['method'],r['path']) in KEYS];self.assertEqual(len(selected),6)
   for row in selected:self.assertEqual((row['response_schema'],row['success_status']),KEYS[row['method'],row['path']]);self.assertEqual(row['role'],'editor')
  spec=importlib.util.spec_from_file_location('share_sync',ROOT/'scripts/sync-v2-openapi.py');module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
  for paths in [json.loads((directory/'openapi.json').read_text())['paths'],module.synchronize({'paths':{}},selected)['paths']]:
   for row in selected:
    op=paths[row['path']][row['method'].lower()];self.assertEqual(op['x-role'],'editor')
    headers={p['name'] for p in op['parameters'] if p['in']=='header'}
    self.assertEqual(headers,{'If-Match'} if row['method']=='PATCH' else {'Idempotency-Key'} if row['idempotency'] else set())
    if row['request_schema']:self.assertEqual(op['requestBody']['content']['application/json']['schema']['$ref'],'schemas.json#/$defs/'+row['request_schema'])
    else:self.assertNotIn('requestBody',op)
   self.assertEqual(set(paths[BASE+'/abandon']['post']['responses']).intersection({'200','201'}),{'200','201'})
 def test_actual_go_empty_slices_optional_times_and_one_time_secret_are_not_conflated(self):
  defs=json.loads((ROOT/'docs/implementation-v2/contracts/schemas.json').read_text())['$defs']
  row=defs['CollectionShare'];self.assertFalse(row['additionalProperties']);self.assertIn('allowed_presets',row['required']);self.assertIn('null',row['properties']['allowed_presets']['type']);self.assertNotIn('expires_at',row['required']);self.assertNotIn('revoked_at',row['required']);self.assertNotIn('secret',row['properties'])
  created=defs['CollectionShareCreated'];self.assertEqual(created['properties']['policy_version'],{'const':'1'});self.assertNotIn('secret',created['required']);self.assertNotIn('revoked_at',created['properties'])
  page=defs['CollectionSharePage'];self.assertEqual(set(page['required']),{'items','next_cursor'});self.assertNotIn('revision',page['properties'])
  self.assertNotIn('expires_at',defs['CollectionSharePatch'].get('required',[]));self.assertIn('null',defs['CollectionSharePatch']['properties']['expires_at']['type'])
