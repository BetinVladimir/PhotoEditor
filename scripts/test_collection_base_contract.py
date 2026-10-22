"""Authored source-contract scenarios only; no service or DB execution implied."""
import json
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]


class CollectionBaseContract(unittest.TestCase):
    def test_real_base_routes_have_exact_packaged_request_and_response_shapes(self):
        base = '/api/v1/tenants/{t}/collections'
        expected = {
            ('GET', base): (None, 'CollectionPage', 200, 'viewer'),
            ('POST', base): ('CollectionCreate', 'CollectionCreated', 201, 'editor'),
            ('GET', base+'/{id}'): (None, 'CollectionResource', 200, 'viewer'),
            ('PATCH', base+'/{id}'): ('CollectionPatch', 'CollectionResource', 200, 'editor'),
            ('DELETE', base+'/{id}'): ('CollectionDeletionRequest', 'CollectionDeletionAccepted', 202, 'editor'),
        }
        schemas = json.loads((ROOT/'docs/implementation-v2/contracts/schemas.json').read_text())['$defs']
        for name in ('docs/implementation-v2/contracts/endpoints.json', 'packages/contracts/v2-endpoints.json', 'packages/contracts/endpoints.json'):
            rows = json.loads((ROOT/name).read_text())['endpoints']
            for identity, shape in expected.items():
                matches = [r for r in rows if (r['method'], r['path']) == identity]
                self.assertEqual(len(matches), 1, (name, identity))
                row = matches[0]
                self.assertEqual((row['request_schema'], row['response_schema'], row['success_status'], row['role']), shape)
                self.assertIn(row['response_schema'], schemas)
        create = schemas['CollectionCreate']
        self.assertIn('project_id', create['required'])
        self.assertNotIn('id', create['properties'])
        self.assertEqual(schemas['CollectionResource']['properties']['revision'], {'$ref': '#/$defs/PositiveInt64Revision'})
        self.assertEqual(schemas['CollectionDeletionAccepted']['properties']['status'], {'const': 'queued'})

    def test_actual_collection_caller_routes_and_cas_headers_are_preserved(self):
        source = (ROOT/'backend/internal/httpapi/catalog.go').read_text()
        for method, suffix in (('Post', ''), ('Get', ''), ('Get', '/{id}'), ('Patch', '/{id}'), ('Delete', '/{id}')):
            self.assertIn('r.'+method+'("/api/v1/tenants/{t}/collections'+suffix+'"', source)
        self.assertNotIn('r.Post("/api/v1/tenants/{t}/projects/{id}/collections"', source)
        api = (ROOT/'apps/photographer/src/collection-metadata-api.ts').read_text()
        self.assertIn("method:'PATCH'", api)
        self.assertIn("method:'DELETE'", api)
        self.assertIn("'If-Match'", api)
        routes = (ROOT/'apps/photographer/src/routes.ts').read_text()
        for method in ('PATCH', 'DELETE'):
            self.assertIn(method+' /api/v1/tenants/{t}/collections/{id}', routes)


if __name__ == '__main__':
    unittest.main()
