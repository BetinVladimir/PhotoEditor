"""Authored exact wire-schema scenarios; not executed in the coding phase."""
import json
import unittest
from pathlib import Path
ROOT = Path(__file__).resolve().parents[1]
class PortraitMaskPairContract(unittest.TestCase):
    def test_reflection_and_local_projection_use_mask_wire_field(self):
        paths = {"/api/v1/tenants/{t}/projects/{id}/reflection-mask-pairs", "/api/v1/tenants/{t}/projects/{id}/local-portrait-mask-pairs"}
        for relative in ("docs/implementation-v2/contracts/endpoints.json", "packages/contracts/v2-endpoints.json", "packages/contracts/endpoints.json"):
            rows = json.loads((ROOT / relative).read_text())["endpoints"]
            found = [r for r in rows if r["method"] == "POST" and r["path"] in paths]
            self.assertEqual(len(found), 2, relative)
            self.assertTrue(all(r["response_schema"] == "ReflectionMaskPairPage" for r in found))
        bundle = json.loads((ROOT / "docs/implementation-v2/contracts/schemas.json").read_text())["$defs"]
        self.assertEqual(set(bundle["ReflectionMaskPair"]["required"]), {"mask", "protected", "target_pixel", "protected_pixel"})
        self.assertFalse(bundle["ReflectionMaskPair"]["additionalProperties"])
        self.assertNotIn("glasses_mask", bundle["ReflectionMaskPair"]["properties"])
        self.assertEqual(bundle["ReflectionMaskPairPage"]["properties"]["items"]["items"], {"$ref": "#/$defs/ReflectionMaskPair"})
        api = json.loads((ROOT / "docs/implementation-v2/contracts/openapi.json").read_text())["paths"]
        for path in paths:
            self.assertEqual(api[path]["post"]["responses"]["200"]["content"]["application/json"]["schema"], {"$ref": "schemas.json#/$defs/ReflectionMaskPairPage"})
        self.assertIn("glasses_mask", bundle["GlareMaskPair"]["properties"])
