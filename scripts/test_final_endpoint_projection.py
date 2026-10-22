"""Authored final exact projection regressions; not executed during coding."""
import importlib.util
import json
from pathlib import Path
import unittest
ROOT=Path(__file__).resolve().parents[1]
MULTI=['/api/v1/tenants/{t}/projects/{id}/'+suffix for suffix in ('panorama-capabilities','panorama-intents','focus-stack-capabilities','focus-stack-intents')]
SHOWN='/api/v1/tenants/{t}/semantic-targets/{target}/version'
ITEM='/api/v1/tenants/{t}/collections/{id}/items/{item}/version'
class FinalEndpointProjection(unittest.TestCase):
    def test_every_current_executable_row_keeps_its_exact_canonical_projection(self):
        self.assertEqual((ROOT/'packages/contracts/v2-endpoints.json').read_text(),(ROOT/'docs/implementation-v2/contracts/endpoints.json').read_text())
        source=json.loads((ROOT/'docs/implementation-v2/contracts/endpoints.json').read_text())['endpoints']
        packaged=json.loads((ROOT/'packages/contracts/endpoints.json').read_text())['endpoints']
        baseline=json.loads((ROOT/'docs/implementation/contracts/endpoints.json').read_text())['endpoints']
        current={(r['method'],r['path']):r for r in packaged}
        native={(r['method'],r['path']):r for r in baseline}
        self.assertEqual([(r['method'],r['path']) for r in packaged],sorted(current))
        for row in source:
            key=row['method'],row['path']
            expected={**row,'origin':['baseline','v2'] if key in native else ['v2'],'contract_format':'executable_schema'}
            if key in native:expected['typed_source_contract']=native[key]
            self.assertEqual(current.get(key),expected,key)

    def test_actual_seven_missing_projections_are_authored_and_reproducible(self):
        rows=json.loads((ROOT/'docs/implementation-v2/contracts/endpoints.json').read_text())['endpoints']
        selected=[r for r in rows if r['path'] in MULTI+[SHOWN,ITEM]]
        self.assertEqual(len(selected),7)
        spec=importlib.util.spec_from_file_location('final_sync',ROOT/'scripts/sync-v2-openapi.py')
        module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
        manual=json.loads((ROOT/'docs/implementation-v2/contracts/openapi.json').read_text())
        for document in [manual,module.synchronize({'paths':{}},selected)]:
            for row in selected:
                operation=document['paths'][row['path']][row['method'].lower()]
                self.assertEqual(operation['x-role'],'editor')
                self.assertIn(str(row['success_status']),operation['responses'])
                if row['path'] in MULTI:
                    self.assertTrue(operation['x-web-mutation-csrf'])
                    keys=[p for p in operation['parameters'] if p['name']=='Idempotency-Key']
                    self.assertEqual(len(keys),int(row['method']=='POST'))
                    if keys:self.assertEqual(keys[0]['schema']['pattern'],'^[!-~]+$')
                if row['path']==SHOWN:
                    self.assertNotIn('requestBody',operation)
                    self.assertTrue(all(p['in']=='path' for p in operation['parameters']))
                if row['path']==ITEM:
                    headers=[p['name'] for p in operation['parameters'] if p['in']=='header']
                    self.assertEqual(headers,['If-Match'] if row['method']=='PUT' else [])
    def test_authoritative_header_metadata_is_kept_in_all_executable_mirrors(self):
        for file in ['docs/implementation-v2/contracts/endpoints.json','packages/contracts/v2-endpoints.json','packages/contracts/endpoints.json']:
            rows=json.loads((ROOT/file).read_text())['endpoints']
            share=next(r for r in rows if r['method']=='PATCH' and r['path']=='/api/v1/tenants/{t}/collections/{id}/shares/{share}')
            self.assertTrue(share['revision_required'])
            for path in MULTI:
                if path.endswith('-intents'):
                    self.assertTrue(next(r for r in rows if r['path']==path)['idempotency'])
if __name__=='__main__':unittest.main()
