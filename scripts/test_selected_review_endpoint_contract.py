"""Selected-review contract scenarios. Authored during coding; run in test phase."""
import json
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]
PREFIX = "/api/v1/tenants/{t}/reviews/{id}"
ROUTES = {
    PREFIX + "/result-selection": "NegativeResultSelection",
    PREFIX + "/render-result-selection": "RenderResultSelection",
    PREFIX + "/selected-preview/{variant}": "ReviewPreview",
}


class SelectedReviewContract(unittest.TestCase):
    def test_exact_editor_projection_in_all_packaged_registries(self):
        for name in (
            "docs/implementation-v2/contracts/endpoints.json",
            "packages/contracts/v2-endpoints.json",
            "packages/contracts/endpoints.json",
        ):
            rows = json.loads((ROOT / name).read_text())["endpoints"]
            for path, response in ROUTES.items():
                with self.subTest(registry=name, path=path):
                    matches = [r for r in rows if r["method"] == "GET" and r["path"] == path]
                    self.assertEqual(len(matches), 1)
                    self.assertEqual(matches[0]["role"], "editor")
                    self.assertIsNone(matches[0]["request_schema"])
                    self.assertEqual(matches[0]["response_schema"], response)
                    self.assertEqual(matches[0]["success_status"], 200)

    def test_openapi_matches_current_editor_and_closed_response_reference(self):
        paths = json.loads((ROOT / "docs/implementation-v2/contracts/openapi.json").read_text())["paths"]
        for path, response in ROUTES.items():
            with self.subTest(path=path):
                operation = paths[path]["get"]
                self.assertEqual(operation["x-role"], "editor")
                self.assertNotIn("requestBody", operation)
                schema = operation["responses"]["200"]["content"]["application/json"]["schema"]
                self.assertEqual(schema, {"$ref": "schemas.json#/$defs/" + response})


if __name__ == "__main__":
    unittest.main()
