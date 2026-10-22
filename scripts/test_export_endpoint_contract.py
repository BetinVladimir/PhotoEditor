"""Exact six-route Export contract scenarios; execute only in test phase."""
import json
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]
PREFIX = "/api/v1/tenants/{t}"
ROWS = [
    ("GET", "/export-presets", None, "ExportPresetPage", "viewer", 200),
    ("POST", "/export-presets", "ExportPresetCreate", "ExportPresetResource", "editor", 201),
    ("PATCH", "/export-presets/{preset}/metadata-policy", "ExportPresetMetadataPolicyChange", "ExportPresetResource", "editor", 200),
    ("POST", "/exports", "ExportBatchCreate", "ExportBatchAccepted", "editor", 202),
    ("GET", "/exports/batches/{id}", None, "ExportBatchStatus", "editor", 200),
    ("GET", "/exports/{id}", None, "ExportArtifactStatus", "viewer", 200),
]


class ExportEndpointContract(unittest.TestCase):
    def test_all_six_current_projections_are_executable_and_not_generic_resources(self):
        for name in ("docs/implementation-v2/contracts/endpoints.json", "packages/contracts/v2-endpoints.json", "packages/contracts/endpoints.json"):
            rows = json.loads((ROOT / name).read_text())["endpoints"]
            for method, suffix, request, response, role, status in ROWS:
                with self.subTest(registry=name, method=method, suffix=suffix):
                    found = [r for r in rows if r["method"] == method and r["path"] == PREFIX + suffix]
                    self.assertEqual(len(found), 1)
                    row = found[0]
                    self.assertEqual(row.get("request_schema"), request)
                    self.assertEqual(row["response_schema"], response)
                    self.assertEqual((row["role"], row["success_status"]), (role, status))

    def test_current_openapi_exact_refs_and_get_without_body(self):
        paths = json.loads((ROOT / "docs/implementation-v2/contracts/openapi.json").read_text())["paths"]
        for method, suffix, request, response, role, status in ROWS:
            with self.subTest(method=method, suffix=suffix):
                operation = paths[PREFIX + suffix][method.lower()]
                self.assertEqual(operation["x-role"], role)
                self.assertEqual(operation["responses"][str(status)]["content"]["application/json"]["schema"], {"$ref": "schemas.json#/$defs/" + response})
                if request is None:
                    self.assertNotIn("requestBody", operation)
                else:
                    self.assertEqual(operation["requestBody"]["content"]["application/json"]["schema"], {"$ref": "schemas.json#/$defs/" + request})


if __name__ == "__main__":
    unittest.main()
