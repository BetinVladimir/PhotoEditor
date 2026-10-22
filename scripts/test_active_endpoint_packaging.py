"""Authored packaging scenarios; not HTTP or runtime qualification."""
import json
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]


class ActiveEndpointPackaging(unittest.TestCase):
    def test_current_advisory_publication_and_usage_remain_discoverable(self):
        expected = {
            ('POST', '/api/v1/tenants/{t}/shoot-sessions/{id}/analysis-capabilities'):
                ('AnalysisCreate', 'SessionAnalysisCapabilities', 'Sessions.AnalysisCapabilities'),
            ('GET', '/api/v1/tenants/{t}/collections/{id}/shares/{share}/usage'):
                (None, 'CollectionShareUsage', 'Share.Usage'),
            ('POST', '/api/v1/tenants/{t}/collections/{id}/publication'):
                ('CollectionPublicationRequest', 'CollectionResource', 'Collection.Publish'),
        }
        for name in ('docs/implementation-v2/contracts/endpoints.json',
                     'packages/contracts/endpoints.json',
                     'packages/contracts/v2-endpoints.json'):
            rows = json.loads((ROOT / name).read_text())['endpoints']
            for identity, shape in expected.items():
                with self.subTest(registry=name, endpoint=identity):
                    matches = [row for row in rows
                               if (row['method'], row['path']) == identity]
                    self.assertEqual(len(matches), 1)
                    row = matches[0]
                    self.assertEqual((row['request_schema'], row['response_schema'],
                                      row['handler']), shape)
                    self.assertEqual(row['success_status'], 200)
                    self.assertEqual(row['role'], 'editor')
                    # Publication uses body revision CAS; the advisory is read-only.
                    # Neither may acquire a fabricated create/receipt contract.
                    self.assertFalse(row.get('idempotency', False))
