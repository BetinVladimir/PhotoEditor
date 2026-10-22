"""Source-authority regressions for the real OpenAPI projection function."""
import copy,importlib.util,json,unittest
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]
spec=importlib.util.spec_from_file_location('source_authority_sync',Path(__file__).with_name('sync-v2-openapi.py'))
module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)

class SourceAuthorityProjection(unittest.TestCase):
 def setUp(self):
  self.document=json.loads((ROOT/'docs/implementation-v2/contracts/openapi.json').read_text())
  self.rows=json.loads((ROOT/'docs/implementation-v2/contracts/endpoints.json').read_text())['endpoints']
  self.projected=module.synchronize(self.document,self.rows)
 def op(self,path,method='get'):return self.projected['paths'][path][method]
 def test_whole_identity_and_idempotent_projection(self):
  identities=lambda x:{(p,m) for p,ops in x['paths'].items() for m in ops if m in ('get','post','put','patch','delete','head','options')}
  self.assertEqual(identities(self.document),identities(self.projected))
  self.assertEqual(len(identities(self.projected)),242)
  self.assertEqual(module.synchronize(self.projected,self.rows),self.projected)
 def test_get_query_contracts_have_no_json_body(self):
  for p in ('/api/v1/tenants/{t}/collections/{id}/apply-capabilities','/api/v1/tenants/{t}/collections/{id}/sections'):
   self.assertNotIn('requestBody',self.op(p));self.assertTrue(any(v['in']=='query' for v in self.op(p)['parameters']))
 def test_exact_replay_statuses_are_not_lost(self):
  cases=[('/api/v1/tenants/{t}/collections/abandon',{'200':'CollectionCreationAbandoned','201':'CollectionCreated'}),('/api/v1/tenants/{t}/collections/{id}/shares/abandon',{'200':'ShareCreationAbandoned','201':'CollectionShareCreated'}),('/api/v1/tenants/{t}/collections/{id}/save-edits',{'200':'CollectionSaveEditsResolution','202':'CollectionApplyAccepted'}),('/api/v1/tenants/{t}/collections/{id}/bulk-operations/abandon',{'200':'CollectionBulkAbandoned','202':'CollectionBulkAcceptedResolution'})]
  for path,codes in cases:
   with self.subTest(path=path):
    responses=self.op(path,'post')['responses']
    for code,schema in codes.items():self.assertEqual(responses[code]['content']['application/json']['schema']['$ref'],'schemas.json#/$defs/'+schema)
 def test_real_response_headers_survive(self):
  for path,method,code,header in [('/api/v1/tenants/{t}/capture-uploads','post','201','ETag'),('/api/v1/tenants/{t}/capture-uploads/{id}','get','200','ETag'),('/api/v1/tenants/{t}/reviews/{id}/previews/{side}/{variant}','get','200','Cache-Control'),('/api/v1/tenants/{t}/collections/{id}/sections','get','200','ETag'),('/api/v1/tenants/{t}/export-presets/{preset}/metadata-policy','patch','200','Idempotency-Replayed')]:
   with self.subTest(path=path):self.assertIn(header,self.op(path,method)['responses'][code]['headers'])
 def test_guest_cookie_is_not_user_bearer(self):
  op=self.op('/api/v1/portal/download-preparations/abandon','post')
  self.assertEqual(op['security'],[{'portalSessionCookie':[],'csrfHeader':[]}]);self.assertTrue(op['x-web-mutation-csrf'])
  self.assertEqual(self.projected['components']['securitySchemes']['portalSessionCookie']['name'],'__Host-photo_portal_session')
 def test_get_mutation_guards_follow_actual_handlers(self):
  for path in ('/api/v1/tenants/{t}/semantic-targets/{target}/preview','/api/v1/tenants/{t}/runs/{id}/local-portrait-result','/api/v1/tenants/{t}/projects/{id}/negative-capabilities'):
   self.assertTrue(self.op(path)['x-web-mutation-csrf'])
 def test_no_invented_error_set_or_dropped_original(self):
  for path,ops in self.document['paths'].items():
   for method,op in ops.items():
    if method not in ('get','post','put','patch','delete','head','options'):continue
    for code,response in op.get('responses',{}).items():
     if code.isdigit() and int(code)>=400:self.assertEqual(self.op(path,method)['responses'][code],response)
  self.assertEqual(self.op('/health/live')['security'],[])
