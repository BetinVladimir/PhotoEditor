"""Authored current analysis advisory projection scenarios; not executed."""
import json
from pathlib import Path
import unittest
ROOT=Path(__file__).resolve().parents[1]
ROUTE='/api/v1/tenants/{t}/styles/{id}/analysis-capabilities'
class StyleAnalysisCapabilityContract(unittest.TestCase):
 def test_exact_existing_route_projection_and_query(self):
  for name in ('docs/implementation-v2/contracts/endpoints.json','packages/contracts/v2-endpoints.json','packages/contracts/endpoints.json'):
   rows=json.loads((ROOT/name).read_text())['endpoints']
   found=[r for r in rows if r['path']==ROUTE and r['method']=='GET']
   self.assertEqual(len(found),1)
   self.assertEqual((found[0]['request_schema'],found[0]['response_schema'],found[0]['role'],found[0]['success_status']),(None,'AnalysisCapabilities','editor',200))
  operation=json.loads((ROOT/'docs/implementation-v2/contracts/openapi.json').read_text())['paths'][ROUTE]['get']
  self.assertNotIn('requestBody',operation)
  self.assertEqual(operation['x-role'],'editor')
  query=[p for p in operation['parameters'] if p['in']=='query']
  self.assertEqual(query,[{'name':'geometry_enabled','in':'query','required':True,'schema':{'type':'boolean'}}])
  self.assertEqual(operation['responses']['200']['content']['application/json']['schema']['$ref'],'schemas.json#/$defs/AnalysisCapabilities')
