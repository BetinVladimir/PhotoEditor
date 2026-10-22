"""Authored static cross-projection checks; never a claim of runtime quality."""
import json
from pathlib import Path
import unittest
ROOT=Path(__file__).resolve().parents[1]
BASE='/api/v1/tenants/{t}/collections/{id}/'
class CollectionApplyContracts(unittest.TestCase):
 def test_four_existing_routes_and_typed_receipts(self):
  expected={('GET','apply-capabilities'):('ApplyCapabilityQuery','ApplyCapabilities'),('POST','apply-profile'):('CollectionApplyRequest','CollectionApplyAccepted'),('POST','save-edits'):('CollectionSaveEditsRequest','CollectionSaveEditsResolution'),('GET','culling-groups'):(None,'CollectionCullingPage')}
  for path in ('docs/implementation-v2/contracts/endpoints.json','packages/contracts/v2-endpoints.json','packages/contracts/endpoints.json'):
   entries=json.loads((ROOT/path).read_text())['endpoints']
   for (method,suffix),(request,response) in expected.items():
    with self.subTest(path=path,suffix=suffix):
     rows=[r for r in entries if r['method']==method and r['path']==BASE+suffix];self.assertEqual(len(rows),1);self.assertEqual(rows[0]['request_schema'],request);self.assertEqual(rows[0]['response_schema'],response)
 def test_same_route_resolve_mode_and_closed_response(self):
  doc=json.loads((ROOT/'docs/implementation-v2/contracts/openapi.json').read_text());op=doc['paths'][BASE+'save-edits']['post']
  headers=[p for p in op['parameters'] if p.get('name')=='Save-Edits-Resolve-Only'];self.assertEqual(len(headers),1);self.assertEqual(headers[0]['schema'],{'const':'abandon'});self.assertIn('200',op['responses']);self.assertIn('202',op['responses'])
  defs=json.loads((ROOT/'docs/implementation-v2/contracts/schemas.json').read_text())['$defs'];resolution=defs['CollectionSaveEditsResolution']['oneOf'];self.assertEqual(resolution[0],{'$ref':'#/$defs/CollectionApplyAccepted'});self.assertFalse(resolution[1]['additionalProperties']);self.assertEqual(set(resolution[1]['required']),{'status','key','collection_id'});self.assertFalse(defs['CollectionApplyAccepted']['additionalProperties'])
