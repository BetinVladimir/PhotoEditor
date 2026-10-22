import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('sync_v2_openapi', Path(__file__).with_name('sync-v2-openapi.py'))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class OpenAPIProjectionTests(unittest.TestCase):
    def test_collection_base_routes_keep_create_key_and_cas_delete_contracts(self):
        base = '/api/v1/tenants/{t}/collections'
        rows = [dict(path=base, method='POST', handler='Collection.Create', role='editor', package='V1', request_schema='CollectionCreate', response_schema='CollectionCreated', success_status=201, description='atomic create'), dict(path=base+'/{id}', method='DELETE', handler='Collection.Archive', role='editor', package='V1', request_schema='CollectionDeletionRequest', response_schema='CollectionDeletionAccepted', success_status=202, description='queued logical cleanup'), dict(path=base, method='GET', handler='Collection.List', role='viewer', package='V1', request_schema=None, response_schema='CollectionPage', success_status=200, description='filtered cursor')]
        result = module.synchronize({'paths': {}}, rows)
        create = result['paths'][base]['post']
        self.assertIn('201', create['responses'])
        self.assertEqual([p['name'] for p in create['parameters'] if p['in'] == 'header'], ['Idempotency-Key'])
        delete = result['paths'][base+'/{id}']['delete']
        self.assertIn('202', delete['responses'])
        self.assertEqual([p['name'] for p in delete['parameters'] if p['in'] == 'header'], ['If-Match'])
        listing = result['paths'][base]['get']
        self.assertNotIn('requestBody', listing)
        self.assertEqual({p['name'] for p in listing['parameters'] if p['in'] == 'query'}, {'limit', 'cursor', 'project_id', 'state', 'search'})
        self.assertEqual(module.synchronize(result, rows), result)

    def test_collection_gallery_queries_are_not_a_get_body(self):
        path = '/api/v1/tenants/{t}/collections/{id}/items'
        endpoint = {'path': path, 'method': 'GET', 'handler': 'Collection.ListItems', 'role': 'viewer', 'package': 'V1', 'request_schema': None, 'query_schema': 'CollectionGalleryQuery', 'response_schema': 'CollectionGalleryPage', 'success_status': 200, 'description': 'Effective revision-bound gallery'}
        original = {'paths': {path: {'get': {'parameters': [{'name': 'obsolete', 'in': 'query'}], 'requestBody': {'required': True}}}}}
        result = module.synchronize(original, [endpoint])
        op = result['paths'][path]['get']
        self.assertNotIn('requestBody', op)
        self.assertEqual({p['name'] for p in op['parameters'] if p['in'] == 'query'}, {'limit', 'cursor', 'section_id', 'flag', 'tag', 'rating', 'search', 'review', 'visibility'})
        self.assertEqual(module.synchronize(result, [endpoint]), result)
        self.assertIn('requestBody', original['paths'][path]['get'])

    def test_binary_response_and_existing_query_are_preserved(self):
        path = '/api/v1/tenants/{t}/preview'
        original = {'paths': {path: {'get': {'parameters': [{'name': 'variant', 'in': 'query'}], 'responses': {'201': {'description': 'old'}}}}}}
        endpoint = {'path': path, 'method': 'GET', 'handler': 'Preview', 'role': 'editor', 'package': 'V30', 'request_schema': None, 'response_schema': 'PNG', 'response_media_type': 'image/png', 'response_max_bytes': 8388608, 'success_status': 200}
        result = module.synchronize(original, [endpoint])
        operation = result['paths'][path]['get']
        self.assertIn('image/png', operation['responses']['200']['content'])
        self.assertNotIn('201', operation['responses'])
        self.assertEqual(operation['responses']['200']['x-max-bytes'], 8388608)
        self.assertEqual(operation['parameters'][0]['name'], 'variant')
        self.assertEqual(module.synchronize(result, [endpoint]), result)
        self.assertIn('201', original['paths'][path]['get']['responses'])

    def test_prepared_decision_requires_uuid_retry_key(self):
        path = '/api/v1/tenants/{t}/projects/{id}/interchange-jobs/{workflow}/prepared-reviews'
        endpoint = {'path': path, 'method': 'POST', 'handler': 'Decide', 'role': 'editor', 'package': 'V30', 'request_schema': 'Decision', 'response_schema': 'Receipt', 'success_status': 200}
        operation = module.synchronize({'paths': {}}, [endpoint])['paths'][path]['post']
        self.assertIn({'name': 'Idempotency-Key', 'in': 'header', 'required': True, 'schema': {'type': 'string', 'format': 'uuid'}}, operation['parameters'])
        self.assertEqual(operation['security'], [{'userBearer': []}])

    def test_import_mask_ids_are_uuid_and_existing_retry_key_is_corrected(self):
        root = '/api/v1/tenants/{t}/projects/{id}/interchange-jobs/{workflow}'
        path = root + '/person-mask-candidates/{entry}/previews/{override}/{mask}'
        endpoint = {'path': path, 'method': 'GET', 'handler': 'Preview', 'role': 'editor', 'package': 'V30', 'request_schema': None, 'response_schema': 'PNG', 'success_status': 200}
        operation = module.synchronize({'paths': {}}, [endpoint])['paths'][path]['get']
        self.assertEqual(len(operation['parameters']), 6)
        for parameter in operation['parameters']:
            self.assertEqual(parameter['schema'], {'type': 'string', 'format': 'uuid'})
        path = root + '/prepared-reviews'
        endpoint.update(path=path, method='POST', request_schema='Decision')
        original = {'paths': {path: {'post': {'parameters': [{'name': 'Idempotency-Key', 'in': 'header', 'schema': {'type': 'string'}}]}}}}
        operation = module.synchronize(original, [endpoint])['paths'][path]['post']
        keys = [p for p in operation['parameters'] if p['name'] == 'Idempotency-Key']
        self.assertEqual(len(keys), 1)
        self.assertEqual(keys[0]['schema']['format'], 'uuid')
        self.assertTrue(keys[0]['required'])

    def test_review_preview_variants_match_server_allowlist(self):
        for suffix in ('versions/{version}/perspective-proposals/{task}', 'panorama-proposals/{task}', 'focus-stack-proposals/{task}'):
            path = '/api/v1/tenants/{t}/projects/{id}/' + suffix + '/previews/{variant}'
            endpoint = {'path': path, 'method': 'GET', 'handler': 'Preview', 'role': 'viewer', 'package': 'V29', 'request_schema': None, 'response_schema': 'Preview', 'success_status': 200}
            operation = module.synchronize({'paths': {}}, [endpoint])['paths'][path]['get']
            for parameter in operation['parameters']:
                if parameter['name'] == 'variant':
                    self.assertEqual(parameter['schema']['enum'], ['preview1600', 'preview2560'])
                else:
                    self.assertEqual(parameter['schema']['format'], 'uuid')

    def test_replacement_apply_does_not_invent_header_idempotency(self):
        path = '/api/v1/tenants/{t}/projects/{id}/panorama-proposals/{task}/apply'
        endpoint = {'path': path, 'method': 'POST', 'handler': 'Apply', 'role': 'editor', 'package': 'V29', 'request_schema': 'Decision', 'response_schema': 'Applied', 'success_status': 200}
        original = {'paths': {path: {'post': {'parameters': [{'name': 'Idempotency-Key', 'in': 'header', 'required': True}]}}}}
        operation = module.synchronize(original, [endpoint])['paths'][path]['post']
        self.assertFalse(any(p.get('name') == 'Idempotency-Key' for p in operation['parameters']))
        self.assertIn('same immutable decision', operation['description'])
        self.assertTrue(operation['requestBody']['required'])

    def test_delivery_revision_and_paired_cursor_contract(self):
        path = '/api/v1/tenants/{t}/delivery-packages/{id}/items'
        endpoint = {'path': path, 'method': 'GET', 'handler': 'Items', 'role': 'viewer', 'package': 'V25', 'request_schema': None, 'response_schema': 'Items', 'success_status': 200}
        operation = module.synchronize({'paths': {}}, [endpoint])['paths'][path]['get']
        parameters = {p['name']: p for p in operation['parameters']}
        self.assertTrue(parameters['revision']['required'])
        self.assertEqual(parameters['limit']['schema']['maximum'], 200)
        self.assertEqual(parameters['after_ordinal']['schema']['maximum'], 4999)
        self.assertEqual(parameters['after_preset']['schema']['format'], 'uuid')
        self.assertEqual(parameters['id']['schema']['format'], 'uuid')
        self.assertIn('supplied together', operation['description'])

    def test_delivery_mutations_accept_printable_keys_not_only_uuid(self):
        for suffix in ('', '/{id}/publish', '/{id}/download-intent'):
            path = '/api/v1/tenants/{t}/delivery-packages' + suffix
            endpoint = {'path': path, 'method': 'POST', 'handler': 'Mutation', 'role': 'editor', 'package': 'V25', 'request_schema': 'Request', 'response_schema': 'Receipt', 'success_status': 200}
            original = {'paths': {path: {'post': {'parameters': [{'name': 'Idempotency-Key', 'in': 'header', 'schema': {'format': 'uuid'}}]}}}}
            result = module.synchronize(original, [endpoint])
            operation = result['paths'][path]['post']
            keys = [p for p in operation['parameters'] if p.get('name') == 'Idempotency-Key']
            self.assertEqual(len(keys), 1)
            self.assertTrue(keys[0]['required'])
            self.assertEqual(keys[0]['schema'], {'type': 'string', 'minLength': 1, 'maxLength': 128, 'pattern': r'^[!-~]+$'})
            self.assertEqual(module.synchronize(result, [endpoint]), result)

    def test_collection_version_cas_uses_if_match_without_retry_ledger(self):
        path = '/api/v1/tenants/{t}/collections/{id}/items/{item}/version'
        for method in ('GET', 'PUT'):
            endpoint = {'path': path, 'method': method, 'handler': 'Version', 'role': 'editor', 'package': 'V29', 'request_schema': 'CollectionItemVersionReplace' if method == 'PUT' else None, 'response_schema': 'CollectionItemVersionState', 'success_status': 200}
            original = {'paths': {path: {method.lower(): {'parameters': [{'name': 'Idempotency-Key', 'in': 'header'}, {'name': 'If-Match', 'in': 'header', 'required': False}]}}}}
            result = module.synchronize(original, [endpoint])
            operation = result['paths'][path][method.lower()]
            self.assertFalse(any(p.get('name') == 'Idempotency-Key' for p in operation['parameters']))
            headers = [p for p in operation['parameters'] if p.get('name') == 'If-Match']
            self.assertEqual(len(headers), 1 if method == 'PUT' else 0)
            if headers:
                self.assertTrue(headers[0]['required'])
            for parameter in operation['parameters']:
                if parameter['in'] == 'path':
                    self.assertEqual(parameter['schema'], {'type': 'string', 'format': 'uuid'})
            self.assertEqual(module.synchronize(result, [endpoint]), result)

    def test_delivery_signing_policy_mimes_are_repeated_bounded_values(self):
        path = '/api/v1/tenants/{t}/projects/{project}/delivery-signing-policies'
        endpoint = {'path': path, 'method': 'GET', 'handler': 'Policies', 'role': 'viewer', 'package': 'V31', 'request_schema': None, 'response_schema': 'DeliverySigningPolicyPage', 'success_status': 200}
        original = {'paths': {path: {'get': {'parameters': [{'name': 'mime', 'in': 'query', 'schema': {'type': 'string'}}, {'name': 'format', 'in': 'query'}]}}}}
        result = module.synchronize(original, [endpoint])
        query = [p for p in result['paths'][path]['get']['parameters'] if p['in'] == 'query']
        self.assertEqual(len(query), 1)
        self.assertEqual(query[0]['name'], 'mime')
        self.assertTrue(query[0]['required'])
        self.assertTrue(query[0]['explode'])
        self.assertEqual(query[0]['schema'], {'type': 'array', 'minItems': 1, 'maxItems': 2, 'uniqueItems': True, 'items': {'type': 'string', 'enum': ['image/jpeg', 'image/png']}})
        self.assertEqual(module.synchronize(result, [endpoint]), result)
