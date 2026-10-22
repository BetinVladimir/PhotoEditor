"""Authored packaging behavior; no generator/application execution."""
import importlib.util
import json
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('endpoint_packaging', Path(__file__).with_name('endpoint_packaging.py'))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class EndpointPackaging(unittest.TestCase):
    def sources(self):
        return (json.loads((ROOT/'docs/implementation/contracts/endpoints.json').read_text()),
                json.loads((ROOT/'docs/implementation-v2/contracts/endpoints.json').read_text()),
                json.loads((ROOT/'docs/implementation-v2/contracts/schemas.json').read_text()))

    def test_real_sources_union_preserves_legacy_and_authoritative_overlaps(self):
        baseline, current, schemas = self.sources()
        value = module.package_endpoints(baseline, current, schemas)
        rows = {(r['method'], r['path']): r for r in value['endpoints']}
        self.assertEqual(set(rows), {(r['method'], r['path']) for r in baseline['endpoints']+current['endpoints']})
        self.assertEqual(len(rows), 323)
        # Actual changed DTO: the old RunAccepted declaration cannot override
        # current batch acceptance or its production handler identity.
        export = rows['POST', '/api/v1/tenants/{t}/exports']
        self.assertEqual(export['response_schema'], 'ExportBatchAccepted')
        self.assertEqual(export['handler'], 'CreateExportBatch')
        self.assertEqual(export['origin'], ['baseline', 'v2'])
        self.assertEqual(export['typed_source_contract']['response'], 'RunAccepted')
        user = rows['GET', '/api/v1/me']
        self.assertEqual(user['contract_format'], 'legacy_typed_source')
        self.assertEqual(user['typed_source_contract']['response'], 'User')
        self.assertIsNone(user['response_schema'])
        generation = rows['POST', '/api/v1/tenants/{t}/generations/abandon']
        self.assertEqual(generation['role'], 'editor')
        self.assertEqual(generation['additional_statuses'], [202])
        self.assertFalse(generation['revision_required'])
        export['typed_source_contract']['response'] = 'mutated'
        self.assertEqual(next(r for r in baseline['endpoints'] if r['handler']=='Export.Create')['response'], 'RunAccepted')

    def test_conflicting_source_duplicates_and_parameter_aliases_refused(self):
        baseline, current, schemas = self.sources()
        original = baseline['endpoints'][0]
        baseline['endpoints'].append(dict(original, status=299))
        with self.assertRaisesRegex(ValueError, 'ENDPOINT_DUPLICATE_CONFLICT'):
            module.package_endpoints(baseline, current, schemas)
        baseline, current, schemas = self.sources()
        row = next(r for r in current['endpoints'] if '{id}' in r['path'])
        current['endpoints'].append(dict(row, path=row['path'].replace('{id}', '{other}')))
        with self.assertRaisesRegex(ValueError, 'ENDPOINT_PARAMETER_ALIAS_CONFLICT'):
            module.package_endpoints(baseline, current, schemas)

    def test_unknown_executable_schema_is_never_silently_downgraded_to_legacy(self):
        baseline, current, schemas = self.sources()
        current['endpoints'][0]['response_schema'] = 'UnregisteredDTO'
        with self.assertRaisesRegex(ValueError, 'ENDPOINT_EXECUTABLE_SCHEMA_UNRESOLVED'):
            module.package_endpoints(baseline, current, schemas)

    def test_native_legacy_artifact_has_one_original_typed_row_per_identity(self):
        baseline, _, _ = self.sources()
        keys = [(r['method'], r['path']) for r in baseline['endpoints']]
        self.assertEqual(len(keys), len(set(keys)))
        self.assertEqual(baseline['format_version'], 1)
        self.assertTrue(all('authorization' in r and 'request' in r and 'response' in r for r in baseline['endpoints']))

    def test_checked_in_artifacts_match_both_explicit_source_formats(self):
        baseline, current, schemas = self.sources()
        packaged = json.loads((ROOT/'packages/contracts/endpoints.json').read_text())
        self.assertEqual(packaged, module.package_endpoints(baseline, current, schemas))
        self.assertEqual((ROOT/'packages/contracts/baseline-endpoints.json').read_text(),
                         (ROOT/'docs/implementation/contracts/endpoints.json').read_text())


if __name__ == '__main__':
    unittest.main()
