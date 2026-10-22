"""Authored source-projection assertions, not executed acceptance."""
import json
from pathlib import Path
import unittest
ROOT=Path(__file__).resolve().parents[1]
class ContactSheetContracts(unittest.TestCase):
 def test_create_and_original_receipt_resolution(self):
  base='/api/v1/tenants/{t}/contact-sheets'
  for name in ['packages/contracts/endpoints.json','packages/contracts/v2-endpoints.json','docs/implementation-v2/contracts/endpoints.json']:
   rows=json.loads((ROOT/name).read_text())['endpoints']
   for suffix,response in [('', 'ContactSheetAccepted'),('/abandon','ContactSheetAbandoned')]:
    with self.subTest(name=name,suffix=suffix):
     found=[r for r in rows if r['method']=='POST' and r['path']==base+suffix];self.assertEqual(len(found),1);self.assertEqual(found[0]['request_schema'],'ContactSheetCreate');self.assertEqual(found[0]['response_schema'],response);self.assertTrue(found[0]['idempotency'])
  doc=json.loads((ROOT/'docs/implementation-v2/contracts/openapi.json').read_text());self.assertIn('202',doc['paths'][base]['post']['responses']);resolution=doc['paths'][base+'/abandon']['post'];self.assertIn('200',resolution['responses']);self.assertIn('202',resolution['responses'])
 def test_actual_supported_layout_and_closed_wire(self):
  defs=json.loads((ROOT/'docs/implementation-v2/contracts/schemas.json').read_text())['$defs'];body=defs['ContactSheetCreate'];self.assertFalse(body['additionalProperties']);self.assertEqual(set(body['required']),{'selection_snapshot_id','locale','columns','rows'});self.assertEqual(body['properties']['columns']['maximum'],8);self.assertEqual(body['properties']['rows']['maximum'],10);self.assertFalse(defs['ContactSheetAccepted']['additionalProperties']);self.assertFalse(defs['ContactSheetAbandoned']['additionalProperties'])

 def test_create_origin_and_actual_authentication_projection(self):
  base='/api/v1/tenants/{t}/contact-sheets'
  rows=json.loads((ROOT/'packages/contracts/endpoints.json').read_text())['endpoints'];row=next(r for r in rows if r['method']=='POST' and r['path']==base);self.assertEqual(row['origin'],['baseline','v2'])
  doc=json.loads((ROOT/'docs/implementation-v2/contracts/openapi.json').read_text());op=doc['paths'][base]['post'];self.assertEqual(op['security'],[{'userBearer':[]}]);self.assertTrue(op['x-web-mutation-csrf'])
  csrf=[p for p in op['parameters'] if p.get('in')=='header' and p.get('name')=='X-CSRF-Token'];self.assertEqual(len(csrf),1);self.assertTrue(csrf[0]['required']);self.assertIn('cookie-authenticated',csrf[0]['description'])
 def test_synchronizer_preserves_conditional_cookie_csrf_and_bearer(self):
  import importlib.util
  spec=importlib.util.spec_from_file_location('contact_sheet_contract_sync',ROOT/'scripts/sync-v2-openapi.py');module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
  base='/api/v1/tenants/{t}/contact-sheets';rows=json.loads((ROOT/'packages/contracts/v2-endpoints.json').read_text())['endpoints'];row=next(r for r in rows if r['method']=='POST' and r['path']==base)
  projected=module.synchronize({'paths':{}},[row]);op=projected['paths'][base]['post'];self.assertEqual(op['security'],[{'userBearer':[]}]);self.assertTrue(op['x-web-mutation-csrf']);csrf=[p for p in op['parameters'] if p.get('name')=='X-CSRF-Token'];self.assertEqual(len(csrf),1);self.assertIn('cookie-authenticated',csrf[0]['description'])
