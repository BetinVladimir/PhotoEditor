"""Authored source-contract boundaries, not API/SQL execution evidence."""
import importlib.util
import json
from pathlib import Path
import unittest

ROOT=Path(__file__).resolve().parents[1]
BASE='/api/v1/tenants/{t}/collections'
KEYS=[('POST',BASE),('POST',BASE+'/abandon'),('POST',BASE+'/{id}/sections'),('POST',BASE+'/{id}/sections/abandon'),('POST',BASE+'/{id}/items'),('POST',BASE+'/{id}/items/{item}/moderation'),('GET',BASE+'/{id}/items/{item}/version'),('PUT',BASE+'/{id}/items/{item}/version')]

class CollectionMutationEndpointContract(unittest.TestCase):
 def test_all_eight_actual_routes_have_closed_projection_and_correct_CAS_transport(self):
  directory=ROOT/'docs/implementation-v2/contracts'
  rows=json.loads((directory/'endpoints.json').read_text())['endpoints']
  paths=json.loads((directory/'openapi.json').read_text())['paths']
  defs=json.loads((directory/'schemas.json').read_text())['$defs']
  selected=[next(row for row in rows if (row['method'],row['path'])==key) for key in KEYS]
  spec=importlib.util.spec_from_file_location('collection_mutation_sync',ROOT/'scripts/sync-v2-openapi.py');module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
  projected=module.synchronize({'paths':{}},selected)['paths']
  for row in selected:
   for source in [paths,projected]:
    op=source[row['path']][row['method'].lower()]
    self.assertEqual(op['x-role'],'editor')
    self.assertEqual(op['responses'][str(row['success_status'])]['content']['application/json']['schema']['$ref'],'schemas.json#/$defs/'+row['response_schema'])
    if row['method']=='GET':self.assertNotIn('requestBody',op)
    else:self.assertEqual(op['requestBody']['content']['application/json']['schema']['$ref'],'schemas.json#/$defs/'+row['request_schema'])
  for suffix in ['/{id}/items','/{id}/sections']:
   op=projected[BASE+suffix]['post'];headers={p['name'] for p in op['parameters'] if p['in']=='header'}
   self.assertEqual(headers,{'Idempotency-Key'})
  moderation=projected[BASE+'/{id}/items/{item}/moderation']['post']
  self.assertFalse(any(p['in']=='header' and p['name'] in ['If-Match','Idempotency-Key'] for p in moderation['parameters']))
  item=defs['CollectionVersionItem'];self.assertFalse(item['additionalProperties'])
  self.assertEqual(item['properties']['rating']['maximum'],5)
  self.assertEqual(item['properties']['position']['maximum'],9007199254740991)
  self.assertTrue(item['properties']['tags']['uniqueItems'])
  self.assertEqual(set(item['required']),{'id','collection_id','asset_id','asset_version_id','position','visibility','moderation_state','rating','flag','tags','created_at'})
  self.assertEqual(defs['CollectionItemModeration']['properties']['decision']['enum'],['accept','reject'])
  self.assertIn('position',defs['CollectionSectionCreate']['required'])
  # ItemAdd deliberately retains the existing optional/null append defaults.
  self.assertNotIn('position',defs['CollectionItemAdd']['required'])
  self.assertIn({'type':'null'},defs['CollectionItemAdd']['properties']['position']['anyOf'])

if __name__=='__main__':unittest.main()
