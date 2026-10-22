"""Capture trust-bootstrap/receipt contracts; authored, not executed."""
import json
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]
PREFIX = "/api/v1/tenants/{t}"
ROWS = {
    "/capture-devices": ("DeviceCreate", "CaptureDeviceCredential", "admin", 201),
    "/capture-devices/{id}/revoke": ("CaptureDeviceRevoke", "CaptureDeviceCredential", "admin", 200),
    "/capture-devices/{id}/challenges": ("ChallengeCreate", "Challenge", "device_public", 200),
    "/capture-devices/{id}/token": ("TokenCreate", "Token", "device_public", 200),
    "/shoot-sessions/{id}/capture-receipts": ("CaptureReceipt", "RowCaptureReceipts", "device", 201),
}

class CaptureDeviceContract(unittest.TestCase):
    def test_actual_flat_sql_projections_in_consumed_registries_and_openapi(self):
        paths = json.loads((ROOT / "docs/implementation-v2/contracts/openapi.json").read_text())["paths"]
        for name in ("docs/implementation-v2/contracts/endpoints.json", "packages/contracts/v2-endpoints.json", "packages/contracts/endpoints.json"):
            rows = json.loads((ROOT / name).read_text())["endpoints"]
            for suffix, (request, response, role, status) in ROWS.items():
                path = PREFIX + suffix
                with self.subTest(registry=name, path=path):
                    matching = [r for r in rows if r["method"] == "POST" and r["path"] == path]
                    self.assertEqual(len(matching), 1)
                    self.assertEqual((matching[0]["request_schema"], matching[0]["response_schema"], matching[0]["role"], matching[0]["success_status"]), (request, response, role, status))
                    operation = paths[path]["post"]
                    self.assertEqual(operation["requestBody"]["content"]["application/json"]["schema"], {"$ref": "schemas.json#/$defs/" + request})
                    self.assertEqual(operation["responses"][str(status)]["content"]["application/json"]["schema"], {"$ref": "schemas.json#/$defs/" + response})

    def test_device_response_is_the_actual_closed_flat_credential_projection(self):
        definitions = json.loads((ROOT / "docs/implementation-v2/contracts/schemas.json").read_text())["$defs"]
        device = definitions["CaptureDeviceCredential"]
        actual = {"id", "revision", "project_id", "label", "public_key", "credential_epoch", "state", "created_at"}
        self.assertEqual(set(device["properties"]), actual)
        self.assertEqual(set(device["required"]), actual)
        self.assertFalse(device["additionalProperties"])
        self.assertEqual(device["properties"]["label"]["maxLength"], 120)
        self.assertEqual(definitions["CaptureDeviceRevoke"]["properties"]["expected_revision"], {"$ref": "#/$defs/PositiveInt64Revision"})
        # Receipt SQL00266 intentionally returns this flat row, never Resource.data.
        self.assertIn("resource_type", definitions["RowCaptureReceipts"]["required"])
        self.assertNotIn("data", definitions["RowCaptureReceipts"]["properties"])

if __name__ == "__main__":
    unittest.main()
