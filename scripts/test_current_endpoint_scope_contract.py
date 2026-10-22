"""Authored source/contract scenarios; no HTTP/SQL execution is implied."""
from pathlib import Path
import json
import unittest

ROOT = Path(__file__).resolve().parents[1]
BASE = '/api/v1/tenants/{t}/capture-uploads'
STATUS = '/api/v1/tenants/{t}/projects/{id}/interchange-jobs/{workflow}/prepared-reviews/{entry}/{target}/{kind}/{proposal}'

class CurrentEndpointScopeContract(unittest.TestCase):
    def setUp(self):
        self.endpoints = json.loads((ROOT/'docs/implementation-v2/contracts/endpoints.json').read_text())['endpoints']
        self.openapi = json.loads((ROOT/'docs/implementation-v2/contracts/openapi.json').read_text())['paths']
        self.defs = json.loads((ROOT/'docs/implementation-v2/contracts/schemas.json').read_text())['$defs']

    def test_device_contracts_do_not_project_browser_authority_or_pagination(self):
        for method, suffix, status, keyed in [('POST','',201,True),('GET','/{id}',200,False),('POST','/{id}/parts',200,False),('POST','/{id}/complete',202,True)]:
            path=BASE+suffix
            row=next(x for x in self.endpoints if x['method']==method and x['path']==path)
            operation=self.openapi[path][method.lower()]
            self.assertEqual(row['role'],'device')
            self.assertEqual(operation['security'],[{'deviceBearer':[]}])
            self.assertFalse(operation['x-web-mutation-csrf'])
            self.assertEqual(row['idempotency'],keyed)
            keys=[x for x in operation['parameters'] if x['name']=='Idempotency-Key']
            self.assertEqual(len(keys),int(keyed))
            self.assertFalse(any(x['in']=='query' for x in operation['parameters']))
            self.assertIn(str(status),operation['responses'])
            self.assertIn(row['response_schema'],self.defs)

    def test_begin_cannot_omit_exact_shoot_session_collection_binding(self):
        begin=self.defs['CaptureDeviceUploadBegin']
        self.assertTrue({'session_id','collection_id','project_id','filename','bytes','format'} <= set(begin['required']))
        self.assertEqual(begin['properties']['bytes']['maximum'],536870912)
        self.assertEqual(self.defs['CaptureDeviceUploadSession']['properties']['revision']['type'],'string')
        self.assertEqual(self.defs['CaptureDeviceUploadPartURLs']['properties']['items']['maxItems'],100)
        self.assertEqual(self.defs['CaptureDeviceUploadAccepted']['properties']['status'],{'const':'queued'})
        # Actual multipart wire spelling is part_no, never part_number.
        part=self.defs['CaptureDeviceUploadComplete']['properties']['parts']['items']
        self.assertEqual(set(part['required']),{'part_no','etag'})

    def test_saved_status_is_single_exact_editor_resource_not_another_list(self):
        operation=self.openapi[STATUS]['get']
        self.assertEqual(operation['security'],[{'userBearer':[]}])
        self.assertEqual(operation['x-role'],'editor')
        parameters={x['name']:x for x in operation['parameters']}
        self.assertEqual(set(parameters),{'t','id','workflow','entry','target','kind','proposal'})
        self.assertEqual(parameters['kind']['schema']['enum'],['defects','dust'])
        self.assertTrue(all(x['required'] and x['in']=='path' for x in parameters.values()))
        self.assertNotIn('requestBody',operation)
        response=operation['responses']['200']['content']['application/json']['schema']['$ref']
        self.assertEqual(response,'schemas.json#/$defs/InterchangeSavedPreparedReview')
        self.assertEqual(self.defs['InterchangeSavedPreparedReview']['properties']['status']['enum'],['granted','revoked'])

    def test_future_projection_from_empty_document_preserves_specialized_authority(self):
        import importlib.util
        spec=importlib.util.spec_from_file_location('scope_sync',ROOT/'scripts/sync-v2-openapi.py')
        module=importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        rows=[x for x in self.endpoints if x['path'].startswith(BASE) or x['path']==STATUS]
        result=module.synchronize({'paths':{}},rows)['paths']
        for row in rows:
            operation=result[row['path']][row['method'].lower()]
            self.assertEqual(operation['security'],[{'deviceBearer' if row['role']=='device' else 'userBearer':[]}])
            for parameter in operation['parameters']:
                if parameter['in']=='path':
                    if parameter['name']=='kind':
                        self.assertEqual(parameter['schema']['enum'],['defects','dust'])
                    else:
                        self.assertEqual(parameter['schema']['format'],'uuid')
            self.assertEqual(any(x['name']=='Idempotency-Key' for x in operation['parameters']),row['idempotency'])

    def test_section_abandon_projection_retains_committed_201_and_tombstone_200(self):
        import importlib.util
        spec=importlib.util.spec_from_file_location('section_scope_sync',ROOT/'scripts/sync-v2-openapi.py')
        module=importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        row=next(x for x in self.endpoints if x['path'].endswith('/collections/{id}/sections/abandon'))
        operation=module.synchronize({'paths':{}},[row])['paths'][row['path']]['post']
        for status,schema in [('200','CollectionSectionAbandoned'),('201','CollectionSection')]:
            self.assertEqual(operation['responses'][status]['content']['application/json']['schema']['$ref'],'schemas.json#/$defs/'+schema)
        key=next(p for p in operation['parameters'] if p['name']=='Idempotency-Key')
        self.assertTrue(key['required'])
        self.assertEqual(key['schema']['maxLength'],128)
