"""Authored TenantAdmin5 projection scenarios; not executed during coding."""
import importlib.util
import json
from pathlib import Path
import unittest
ROOT=Path(__file__).resolve().parents[1]
BASE='/api/v1/tenants/{t}'
ROWS={('DELETE',BASE):(None,'TenantAdminDeletionAccepted',202,'owner'),('POST',BASE+'/transfer-owner'):('TenantAdminTransferOwner','TenantAdminResource',200,'owner'),('POST',BASE+'/invites'):('TenantAdminInvite','TenantAdminInvitation',201,'admin'),('DELETE',BASE+'/members/{user}'):(None,None,204,'admin'),('GET',BASE+'/events'):(None,'TenantAdminEventPage',200,'viewer')}
class TenantAdminContracts(unittest.TestCase):
 def test_five_actual_routes_and_projection_preserve_json_sse_and_204(self):
  directory=ROOT/'docs/implementation-v2/contracts'
  for file in ['docs/implementation-v2/contracts/endpoints.json','packages/contracts/endpoints.json','packages/contracts/v2-endpoints.json']:
   selected=[r for r in json.loads((ROOT/file).read_text())['endpoints'] if (r['method'],r['path']) in ROWS]
   self.assertEqual(len(selected),5)
   for row in selected:self.assertEqual((row.get('request_schema'),row['response_schema'],row['success_status'],row['role']),ROWS[row['method'],row['path']])
  spec=importlib.util.spec_from_file_location('tenant_sync',ROOT/'scripts/sync-v2-openapi.py');module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
  for paths in [json.loads((directory/'openapi.json').read_text())['paths'],module.synchronize({'paths':{}},selected)['paths']]:
   for (method,path),(request,response,status,role) in ROWS.items():
    op=paths[path][method.lower()];self.assertEqual(op['x-role'],role)
    headers={p['name'] for p in op['parameters'] if p['in']=='header'}
    self.assertEqual(headers,{'If-Match'} if method=='DELETE' else {'Idempotency-Key'} if path.endswith('/invites') else {'Last-Event-ID'} if path.endswith('/events') else set())
    if request:self.assertEqual(op['requestBody']['content']['application/json']['schema']['$ref'],'schemas.json#/$defs/'+request)
    else:self.assertNotIn('requestBody',op)
    if status==204:self.assertNotIn('content',op['responses']['204'])
   events=paths[BASE+'/events']['get'];self.assertEqual(set(events['responses']['200']['content']),{'application/json','text/event-stream'})
   self.assertEqual(events['responses']['200']['content']['text/event-stream']['x-event-schema']['$ref'],'schemas.json#/$defs/TenantAdminEvent')
 def test_wire_omission_null_and_legacy_tenant_json_are_distinct(self):
  defs=json.loads((ROOT/'docs/implementation-v2/contracts/schemas.json').read_text())['$defs']
  self.assertEqual(set(defs['TenantAdminTransferOwner']['required']),{'target_user_id','expected_revision'})
  self.assertEqual(set(defs['TenantAdminInvitation']['required']),{'id','email','role','expires_at'})
  self.assertNotIn('quotas',defs['TenantAdminResource']['required'])
  self.assertNotIn('additionalProperties',defs['TenantAdminResource']['properties']['quotas'])
  self.assertEqual(set(defs['TenantAdminEventPage']['required']),{'items','next_cursor'})
  for key in ['run_id','collection_id']:
   self.assertIn(key,defs['TenantAdminEvent']['required']);self.assertIn({'type':'null'},defs['TenantAdminEvent']['properties'][key]['anyOf'])
